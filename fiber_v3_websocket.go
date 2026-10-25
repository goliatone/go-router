package router

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

// fiberV3WebSocketContext implements WebSocketContext for Fiber
type fiberV3WebSocketContext struct {
	*fiberV3Context
	config           WebSocketConfig
	conn             *websocket.Conn
	isUpgraded       bool
	mu               sync.RWMutex
	writeMu          sync.Mutex
	connectionID     string
	subprotocol      string
	closeHandlers    []func(code int, text string) error
	pingHandler      func(data []byte) error
	pongHandler      func(data []byte) error
	messageHandler   func(messageType int, data []byte) error
	upgradeData      UpgradeData
	userCtx          context.Context
	readInterrupted  bool
	readInterruptErr error
}

// Ensure fiberV3WebSocketContext implements WebSocketContext
var _ WebSocketContext = (*fiberV3WebSocketContext)(nil)
var _ WebSocketReadInterrupter = (*fiberV3WebSocketContext)(nil)

// NewFiberV3WebSocketContext creates a new Fiber WebSocket context
func NewFiberV3WebSocketContext(c fiber.Ctx, config WebSocketConfig, logger Logger) (*fiberV3WebSocketContext, error) {
	// Create base fiber context
	baseCtx, ok := NewFiberV3Context(c, logger).(*fiberV3Context)
	if !ok {
		return nil, fmt.Errorf("expected fiberV3Context from NewFiberV3Context")
	}

	// Generate connection ID
	connID := fmt.Sprintf("fiber-ws-%s-%d", c.IP(), time.Now().UnixNano())

	return &fiberV3WebSocketContext{
		fiberV3Context: baseCtx,
		config:         config,
		connectionID:   connID,
		closeHandlers:  make([]func(code int, text string) error, 0),
		userCtx:        baseCtx.Context(),
	}, nil
}

// IsWebSocket returns true if the connection has been upgraded to WebSocket
func (c *fiberV3WebSocketContext) IsWebSocket() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isUpgraded
}

// Context returns a safe per-connection context even after the fasthttp
// request context has been hijacked by the websocket upgrader.
func (c *fiberV3WebSocketContext) Context() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.isUpgraded {
		if c.userCtx != nil {
			return c.userCtx
		}
		return context.Background()
	}

	return c.fiberV3Context.Context()
}

// SetContext stores user context without touching the hijacked fasthttp
// RequestCtx after the websocket upgrade.
func (c *fiberV3WebSocketContext) SetContext(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	if c.isUpgraded {
		c.userCtx = ctx
		return
	}

	c.fiberV3Context.SetContext(ctx)
	c.userCtx = ctx
}

// WebSocketUpgrade upgrades the HTTP connection to WebSocket
func (c *fiberV3WebSocketContext) WebSocketUpgrade() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isUpgraded {
		return nil // Already upgraded
	}

	if c.ctx == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("context unavailable"))
	}

	// Check if this is a WebSocket request
	if c.ctx.Get("Upgrade") != "websocket" {
		return fmt.Errorf("not a WebSocket upgrade request")
	}

	// Check WebSocket version header
	if c.ctx.Get("Sec-WebSocket-Version") == "" {
		return fmt.Errorf("invalid WebSocket upgrade request")
	}

	// This will be handled by the WebSocket middleware
	// The actual upgrade happens in the FiberV3WebSocketHandler
	c.isUpgraded = true

	return nil
}

// WriteMessage sends a message to the WebSocket connection
func (c *fiberV3WebSocketContext) WriteMessage(messageType int, data []byte) error {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	c.mu.RUnlock()

	if !upgraded || conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set write deadline
	writeTimeout := c.getWriteTimeout()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if writeTimeout > 0 {
		deadline := time.Now().Add(writeTimeout)
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}

	return conn.WriteMessage(messageType, data)
}

// ReadMessage reads a message from the WebSocket connection
func (c *fiberV3WebSocketContext) ReadMessage() (messageType int, p []byte, err error) {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	interrupted := c.readInterrupted
	readDeadlineEnabled := c.config.readDeadlineEnabled()
	pongWait := c.config.PongWait
	messageHandler := c.messageHandler
	onMessage := c.config.OnMessage
	if !upgraded || conn == nil {
		c.mu.RUnlock()
		return 0, nil, ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}
	if interrupted {
		c.mu.RUnlock()
		return 0, nil, ErrWebSocketReadInterrupted
	}

	// Set read deadline for next message
	if readDeadlineEnabled {
		deadline := time.Now().Add(pongWait)
		if deadlineErr := conn.SetReadDeadline(deadline); deadlineErr != nil {
			c.mu.RUnlock()
			return 0, nil, deadlineErr
		}
	}
	c.mu.RUnlock()

	messageType, p, err = conn.ReadMessage()
	if err != nil && c.isReadInterrupted() {
		err = ErrWebSocketReadInterrupted
	}

	// Call message handler if set
	if err == nil && messageHandler != nil {
		if handlerErr := messageHandler(messageType, p); handlerErr != nil {
			// Log error but don't fail the read
			c.logger.Info("Message handler error", "error", handlerErr)
		}
	}

	// Call OnMessage handler if configured
	if err == nil && onMessage != nil {
		if handlerErr := onMessage(c, messageType, p); handlerErr != nil {
			// Log error but don't fail the read
			c.logger.Info("OnMessage handler error", "error", handlerErr)
		}
	}

	return messageType, p, err
}

// WriteJSON sends a JSON message
func (c *fiberV3WebSocketContext) WriteJSON(v any) error {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	c.mu.RUnlock()

	if !upgraded || conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set write deadline
	writeTimeout := c.getWriteTimeout()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if writeTimeout > 0 {
		deadline := time.Now().Add(writeTimeout)
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}

	return conn.WriteJSON(v)
}

// ReadJSON reads a JSON message
func (c *fiberV3WebSocketContext) ReadJSON(v any) error {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	interrupted := c.readInterrupted
	readDeadlineEnabled := c.config.readDeadlineEnabled()
	pongWait := c.config.PongWait

	if !upgraded || conn == nil {
		c.mu.RUnlock()
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}
	if interrupted {
		c.mu.RUnlock()
		return ErrWebSocketReadInterrupted
	}

	// Set read deadline
	if readDeadlineEnabled {
		deadline := time.Now().Add(pongWait)
		if err := conn.SetReadDeadline(deadline); err != nil {
			c.mu.RUnlock()
			return err
		}
	}
	c.mu.RUnlock()

	err := conn.ReadJSON(v)
	if err != nil && c.isReadInterrupted() {
		return ErrWebSocketReadInterrupted
	}
	return err
}

// InterruptRead terminally interrupts the connection's read side without
// calling a WebSocket reader method from a second goroutine.
func (c *fiberV3WebSocketContext) InterruptRead() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.readInterrupted {
		return c.readInterruptErr
	}
	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	c.readInterrupted = true
	c.readInterruptErr = interruptWebSocketRead(c.conn.NetConn())
	return c.readInterruptErr
}

func (c *fiberV3WebSocketContext) isReadInterrupted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.readInterrupted
}

// Close closes the WebSocket connection
func (c *fiberV3WebSocketContext) Close() error {
	return c.CloseWithStatus(CloseNormalClosure, "")
}

// CloseWithStatus closes the WebSocket connection with a status code and reason
func (c *fiberV3WebSocketContext) CloseWithStatus(code int, reason string) error {
	c.mu.Lock()
	if !c.isUpgraded || c.conn == nil {
		c.mu.Unlock()
		return nil // Not connected
	}
	conn := c.conn
	onDisconnect := c.config.OnDisconnect
	closeHandlers := append([]func(code int, text string) error{}, c.closeHandlers...)
	c.isUpgraded = false
	c.conn = nil
	c.mu.Unlock()

	// Call OnDisconnect handler if configured
	if onDisconnect != nil {
		onDisconnect(c, nil)
	}

	// Call close handlers
	for _, handler := range closeHandlers {
		if err := handler(code, reason); err != nil {
			c.logger.Info("Close handler error", "error", err)
		}
	}

	// Send close message
	message := websocket.FormatCloseMessage(code, reason)
	writeTimeout := c.getWriteTimeout()
	deadline := time.Now().Add(writeTimeout)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := conn.SetWriteDeadline(deadline); err != nil {
		// Still try to close the connection
		if closeErr := conn.Close(); closeErr != nil {
			c.logger.Info("WebSocket close after deadline error failed", "error", closeErr)
		}
		return err
	}

	if err := conn.WriteMessage(websocket.CloseMessage, message); err != nil {
		// Still close the connection
		if closeErr := conn.Close(); closeErr != nil {
			c.logger.Info("WebSocket close after close-message error failed", "error", closeErr)
		}
		return err
	}

	// Close the connection
	if err := conn.Close(); err != nil {
		return err
	}

	return nil
}

// SetReadDeadline sets the read deadline for the connection
func (c *fiberV3WebSocketContext) SetReadDeadline(t time.Time) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	conn := c.conn
	upgraded := c.isUpgraded

	if !upgraded || conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}
	if c.readInterrupted {
		return ErrWebSocketReadInterrupted
	}

	return conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline for the connection
func (c *fiberV3WebSocketContext) SetWriteDeadline(t time.Time) error {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	c.mu.RUnlock()

	if !upgraded || conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return conn.SetWriteDeadline(t)
}

// WritePing sends a ping message
func (c *fiberV3WebSocketContext) WritePing(data []byte) error {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	c.mu.RUnlock()

	if !upgraded || conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set write deadline
	writeTimeout := c.getWriteTimeout()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if writeTimeout > 0 {
		deadline := time.Now().Add(writeTimeout)
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}

	return conn.WriteMessage(websocket.PingMessage, data)
}

// WritePong sends a pong message
func (c *fiberV3WebSocketContext) WritePong(data []byte) error {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	c.mu.RUnlock()

	if !upgraded || conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set write deadline
	writeTimeout := c.getWriteTimeout()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if writeTimeout > 0 {
		deadline := time.Now().Add(writeTimeout)
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}

	return conn.WriteMessage(websocket.PongMessage, data)
}

// SetPingHandler sets the ping handler
func (c *fiberV3WebSocketContext) SetPingHandler(handler func(data []byte) error) {
	c.mu.Lock()
	c.pingHandler = handler
	conn := c.conn
	c.mu.Unlock()

	if conn != nil {
		c.installPingHandler(conn)
	}
}

// SetPongHandler sets the pong handler
func (c *fiberV3WebSocketContext) SetPongHandler(handler func(data []byte) error) {
	c.mu.Lock()
	c.pongHandler = handler
	conn := c.conn
	c.mu.Unlock()

	if conn != nil {
		c.installPongHandler(conn)
	}
}

func (c *fiberV3WebSocketContext) installPingHandler(conn *websocket.Conn) {
	if conn == nil {
		return
	}

	conn.SetPingHandler(func(appData string) error {
		writeTimeout := c.getWriteTimeout()
		deadline := time.Now().Add(writeTimeout)
		c.writeMu.Lock()
		if err := conn.SetWriteDeadline(deadline); err != nil {
			c.writeMu.Unlock()
			return err
		}
		if err := conn.WriteMessage(websocket.PongMessage, []byte(appData)); err != nil {
			c.writeMu.Unlock()
			return err
		}
		c.writeMu.Unlock()

		c.mu.RLock()
		pingHandler := c.pingHandler
		c.mu.RUnlock()
		if pingHandler != nil {
			return pingHandler([]byte(appData))
		}
		return nil
	})
}

func (c *fiberV3WebSocketContext) installPongHandler(conn *websocket.Conn) {
	if conn == nil {
		return
	}

	conn.SetPongHandler(func(appData string) error {
		c.mu.Lock()
		if c.readInterrupted {
			c.mu.Unlock()
			return ErrWebSocketReadInterrupted
		}
		if c.config.readDeadlineEnabled() {
			deadline := time.Now().Add(c.config.PongWait)
			if err := conn.SetReadDeadline(deadline); err != nil {
				c.mu.Unlock()
				return err
			}
		}

		pongHandler := c.pongHandler
		c.mu.Unlock()
		if pongHandler != nil {
			return pongHandler([]byte(appData))
		}
		return nil
	})
}

// SetCloseHandler sets the close handler
func (c *fiberV3WebSocketContext) SetCloseHandler(handler func(code int, text string) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeHandlers = append(c.closeHandlers, handler)
}

// Subprotocol returns the negotiated subprotocol
func (c *fiberV3WebSocketContext) Subprotocol() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subprotocol
}

// Extensions returns the negotiated extensions
func (c *fiberV3WebSocketContext) Extensions() []string {
	return []string{}
}

// RemoteAddr returns the remote address of the connection
func (c *fiberV3WebSocketContext) RemoteAddr() string {
	if c.fiberV3Context == nil {
		return ""
	}
	return c.IP()
}

// LocalAddr returns the local address of the connection
func (c *fiberV3WebSocketContext) LocalAddr() string {
	if c.fiberV3Context == nil {
		return ""
	}
	if meta := c.getMeta(); meta != nil {
		return fmt.Sprintf("%s:%s", meta.host, meta.port)
	}
	if ctx := c.liveCtx(); ctx != nil {
		return fmt.Sprintf("%s:%s", ctx.Hostname(), ctx.Port())
	}
	return ""
}

// IsConnected returns true if the WebSocket is connected
func (c *fiberV3WebSocketContext) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isUpgraded && c.conn != nil
}

// ConnectionID returns a unique identifier for this connection
func (c *fiberV3WebSocketContext) ConnectionID() string {
	return c.connectionID
}

// UpgradeData returns pre-upgrade data, if available
func (c *fiberV3WebSocketContext) UpgradeData(key string) (any, bool) {
	if c.upgradeData == nil {
		return nil, false
	}
	val, ok := c.upgradeData[key]
	return val, ok
}

func (c *fiberV3WebSocketContext) setHandlerIndex(index int) { c.index = index }

func (c *fiberV3WebSocketContext) Next() error {
	c.index++
	if c.index >= len(c.handlers) {
		return nil
	}
	return c.handlers[c.index].Handler(c)
}

// FiberV3WebSocketFactory implements WebSocketContextFactory for Fiber
type FiberV3WebSocketFactory struct {
	logger Logger
}

// NewFiberV3WebSocketFactory creates a new Fiber WebSocket factory
func NewFiberV3WebSocketFactory(logger Logger) *FiberV3WebSocketFactory {
	return &FiberV3WebSocketFactory{
		logger: logger,
	}
}

// CreateWebSocketContext creates a Fiber specific WebSocket context
// Note: This factory is mainly for testing. In production, use FiberV3WebSocketHandler
func (f *FiberV3WebSocketFactory) CreateWebSocketContext(c Context, config WebSocketConfig) (WebSocketContext, error) {
	// Ensure it's a Fiber context
	fiberCtx, ok := c.(*fiberV3Context)
	if !ok {
		return nil, fmt.Errorf("expected fiberV3Context, got %T", c)
	}

	// Create WebSocket context (without actual connection for factory pattern)
	wsCtx, err := NewFiberV3WebSocketContext(fiberCtx.ctx, config, f.logger)
	if err != nil {
		return nil, err
	}

	// Note: Actual WebSocket upgrade should happen through FiberV3WebSocketHandler
	// This factory method is mainly for interface compatibility and testing
	if err := wsCtx.WebSocketUpgrade(); err != nil {
		return nil, err
	}

	return wsCtx, nil
}

// SupportsWebSocket returns true as Fiber supports WebSockets
func (f *FiberV3WebSocketFactory) SupportsWebSocket() bool {
	return true
}

// AdapterName returns the adapter name
func (f *FiberV3WebSocketFactory) AdapterName() string {
	return "fiber-v3"
}

// RegisterFiberV3WebSocketFactory registers the Fiber WebSocket factory globally
func RegisterFiberV3WebSocketFactory(logger Logger) {
	if logger == nil {
		logger = &defaultLogger{}
	}
	factory := NewFiberV3WebSocketFactory(logger)
	RegisterWebSocketFactory("fiber-v3", factory)
}

func init() {
	// Ensure Fiber adapters have a WebSocket factory available without manual registration.
	RegisterFiberV3WebSocketFactory(nil)
}

// FiberV3WebSocketHandler creates a Fiber-specific WebSocket handler using contrib/websocket
func FiberV3WebSocketHandler(config WebSocketConfig, handler func(WebSocketContext) error) fiber.Handler {
	upgrade := fiberV3WebSocketHandler(config, handler)
	return func(c fiber.Ctx) error {
		return upgrade(NewFiberV3Context(c, &defaultLogger{}).(*fiberV3Context))
	}
}

// Both router routes and the native helper use one context from pre-upgrade
// hooks through capture. Router routes supply their existing middleware state.
func fiberV3WebSocketHandler(config WebSocketConfig, handler func(WebSocketContext) error) func(*fiberV3Context) error {
	// Apply defaults to config
	config.ApplyDefaults()
	configErr := config.Validate()

	// Return a wrapper function that delegates to WebSocket
	return func(ctx *fiberV3Context) error {
		c := ctx.liveCtx()
		if c == nil {
			return fmt.Errorf("context unavailable")
		}
		if configErr != nil {
			return fmt.Errorf("websocket configuration error: %w", configErr)
		}
		if !validateFiberV3WebSocketOrigin(c, config) {
			return c.SendStatus(fiber.StatusForbidden)
		}

		// 1. Handle OnPreUpgrade if configured
		var upgradeData UpgradeData
		if config.OnPreUpgrade != nil {
			// Execute the hook
			var err error
			upgradeData, err = config.OnPreUpgrade(ctx)
			if err != nil {
				return writeWebSocketPreUpgradeError(ctx, err)
			}
		}

		return runFiberV3WebSocketUpgrade(c, ctx.logger, ctx, config, upgradeData, handler)
	}
}

func handleFiberV3MiddlewareWebSocketUpgrade(c Context, config WebSocketConfig, upgradeData UpgradeData) error {
	fiberCtx, ok := c.(*fiberV3Context)
	if !ok || fiberCtx == nil || fiberCtx.ctx == nil {
		return fmt.Errorf("expected fiberV3Context, got %T", c)
	}

	return runFiberV3WebSocketUpgrade(fiberCtx.ctx, fiberCtx.logger, fiberCtx, config, upgradeData, func(ws WebSocketContext) error {
		return ws.Next()
	})
}

// Router origin validation runs before this upgrader. Allow it through here
// so contrib does not apply a second conflicting empty-origin/custom policy.
func buildFiberV3WebSocketConfig(config WebSocketConfig) websocket.Config {
	return websocket.Config{
		ReadBufferSize: config.ReadBufferSize, WriteBufferSize: config.WriteBufferSize,
		Subprotocols: config.Subprotocols, HandshakeTimeout: config.HandshakeTimeout,
		EnableCompression: config.EnableCompression, Origins: []string{"*"}, AllowEmptyOrigin: true,
	}
}

func captureFiberV3ContextForWebSocket(c fiber.Ctx, logger Logger, prototype *fiberV3Context) (*fiberV3Context, context.Context, error) {
	if logger == nil {
		logger = &defaultLogger{}
	}

	safeCtx := c.Context()
	if prototype != nil && prototype.cachedCtx != nil {
		safeCtx = prototype.cachedCtx
	}
	if safeCtx == nil {
		safeCtx = context.Background()
	}

	captured, ok := NewFiberV3Context(c, logger).(*fiberV3Context)
	if !ok {
		return nil, safeCtx, fmt.Errorf("expected fiberV3Context from NewFiberV3Context")
	}
	if prototype != nil {
		captured.mergeStrategy = prototype.mergeStrategy
		captured.handlers = prototype.handlers
		captured.index = prototype.index
		captured.store = prototype.store
		if prototype.logger != nil {
			captured.logger = prototype.logger
		}
		if prototype.meta != nil {
			captured.meta = prototype.meta
		}
	}
	captured.SetContext(safeCtx)
	captured.captureRequestMeta()
	captured.snapshotLocals(c)
	// The upgrader callback may run after native Ctx recycling. No live native
	// context, request or response writer can escape into that callback.
	captured.ctx = nil
	captured.httpReq = nil
	captured.httpRes = nil
	return captured, safeCtx, nil
}

func runFiberV3WebSocketUpgrade(c fiber.Ctx, logger Logger, prototype *fiberV3Context, config WebSocketConfig, upgradeData UpgradeData, invoke func(WebSocketContext) error) error {
	if logger == nil {
		logger = &defaultLogger{}
	}

	wsConfig := buildFiberV3WebSocketConfig(config)
	capturedFiberCtx, safeCtx, err := captureFiberV3ContextForWebSocket(c, logger, prototype)
	if err != nil {
		return err
	}

	wsHandler := websocket.New(func(conn *websocket.Conn) {
		baseWsCtx := &fiberV3WebSocketContext{
			fiberV3Context: capturedFiberCtx,
			config:         config,
			conn:           conn,
			isUpgraded:     true,
			connectionID:   fmt.Sprintf("fiber-ws-%s-%d", conn.RemoteAddr().String(), time.Now().UnixNano()),
			closeHandlers:  make([]func(code int, text string) error, 0),
			upgradeData:    upgradeData,
			userCtx:        safeCtx,
		}

		if config.MaxMessageSize > 0 {
			conn.SetReadLimit(config.MaxMessageSize)
		}

		defer baseWsCtx.Close()

		baseWsCtx.installPingHandler(conn)
		baseWsCtx.installPongHandler(conn)

		conn.SetCloseHandler(func(code int, text string) error {
			baseWsCtx.mu.RLock()
			closeHandlers := append([]func(code int, text string) error{}, baseWsCtx.closeHandlers...)
			baseWsCtx.mu.RUnlock()
			for _, closeHandler := range closeHandlers {
				if err := closeHandler(code, text); err != nil {
					logger.Info("Close handler error", "error", err)
				}
			}
			return nil
		})

		baseWsCtx.subprotocol = conn.Subprotocol()

		if config.readDeadlineEnabled() {
			deadline := time.Now().Add(config.PongWait)
			if err := conn.SetReadDeadline(deadline); err != nil {
				logger.Info("WebSocket read deadline error", "error", err)
				if closeErr := conn.Close(); closeErr != nil {
					logger.Info("WebSocket close after read deadline error failed", "error", closeErr)
				}
				return
			}
		}

		stopPingLoop := startWebSocketPingLoop(baseWsCtx, config, logger)
		defer stopPingLoop()

		if config.OnConnect != nil {
			if err := config.OnConnect(baseWsCtx); err != nil {
				logger.Info("OnConnect handler error", "error", err)
				if closeErr := conn.Close(); closeErr != nil {
					logger.Info("WebSocket close after OnConnect error failed", "error", closeErr)
				}
				return
			}
		}

		if err := invoke(baseWsCtx); err != nil {
			logger.Info("WebSocket handler error", "error", err)
		}
	}, wsConfig)

	return wsHandler(c)
}

// getWriteTimeout returns the write timeout to use
func (c *fiberV3WebSocketContext) getWriteTimeout() time.Duration {
	if c.config.WriteTimeout > 0 {
		return c.config.WriteTimeout
	}
	return 10 * time.Second // Default
}

// RouteName returns the route name from context
func (c *fiberV3WebSocketContext) RouteName() string {
	if name, ok := RouteNameFromContext(c.Context()); ok {
		return name
	}
	return ""
}

// RouteParams returns all route parameters as a map
func (c *fiberV3WebSocketContext) RouteParams() map[string]string {
	if params, ok := RouteParamsFromContext(c.Context()); ok {
		return params
	}
	return make(map[string]string)
}

// validateFiberV3Origin validates the origin for Fiber WebSocket requests
func validateFiberV3Origin(c fiber.Ctx, allowedOrigins []string) bool {
	origin := strings.TrimSpace(c.Get("Origin"))
	if len(allowedOrigins) == 0 {
		if origin == "" {
			return true
		}
		ctx := NewFiberV3Context(c, nil)
		return originMatchesRequest(origin, requestScheme(ctx), requestHost(ctx))
	}
	return matchesAnyOriginPattern(origin, allowedOrigins)
}

func validateFiberV3WebSocketOrigin(c fiber.Ctx, config WebSocketConfig) bool {
	if c == nil {
		return false
	}
	if config.CheckOrigin != nil {
		return config.CheckOrigin(strings.TrimSpace(c.Get("Origin")))
	}
	return validateFiberV3Origin(c, config.Origins)
}
