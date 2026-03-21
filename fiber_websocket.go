package router

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

// fiberWebSocketContext implements WebSocketContext for Fiber
type fiberWebSocketContext struct {
	*fiberContext
	config         WebSocketConfig
	conn           *websocket.Conn
	isUpgraded     bool
	mu             sync.RWMutex
	writeMu        sync.Mutex
	connectionID   string
	subprotocol    string
	closeHandlers  []func(code int, text string) error
	pingHandler    func(data []byte) error
	pongHandler    func(data []byte) error
	messageHandler func(messageType int, data []byte) error
	upgradeData    UpgradeData
	userCtx        context.Context
}

// Ensure fiberWebSocketContext implements WebSocketContext
var _ WebSocketContext = (*fiberWebSocketContext)(nil)

// NewFiberWebSocketContext creates a new Fiber WebSocket context
func NewFiberWebSocketContext(c *fiber.Ctx, config WebSocketConfig, logger Logger) (*fiberWebSocketContext, error) {
	// Create base fiber context
	baseCtx := NewFiberContext(c, logger).(*fiberContext)

	// Generate connection ID
	connID := fmt.Sprintf("fiber-ws-%s-%d", c.IP(), time.Now().UnixNano())

	return &fiberWebSocketContext{
		fiberContext:  baseCtx,
		config:        config,
		connectionID:  connID,
		closeHandlers: make([]func(code int, text string) error, 0),
		userCtx:       baseCtx.Context(),
	}, nil
}

// IsWebSocket returns true if the connection has been upgraded to WebSocket
func (c *fiberWebSocketContext) IsWebSocket() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isUpgraded
}

// Context returns a safe per-connection context even after the fasthttp
// request context has been hijacked by the websocket upgrader.
func (c *fiberWebSocketContext) Context() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.isUpgraded {
		if c.userCtx != nil {
			return c.userCtx
		}
		return context.Background()
	}

	return c.fiberContext.Context()
}

// SetContext stores user context without touching the hijacked fasthttp
// RequestCtx after the websocket upgrade.
func (c *fiberWebSocketContext) SetContext(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	if c.isUpgraded {
		c.userCtx = ctx
		return
	}

	c.fiberContext.SetContext(ctx)
	c.userCtx = ctx
}

// WebSocketUpgrade upgrades the HTTP connection to WebSocket
func (c *fiberWebSocketContext) WebSocketUpgrade() error {
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
	// The actual upgrade happens in the FiberWebSocketHandler
	c.isUpgraded = true

	return nil
}

// WriteMessage sends a message to the WebSocket connection
func (c *fiberWebSocketContext) WriteMessage(messageType int, data []byte) error {
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
func (c *fiberWebSocketContext) ReadMessage() (messageType int, p []byte, err error) {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	pongWait := c.config.PongWait
	messageHandler := c.messageHandler
	onMessage := c.config.OnMessage
	c.mu.RUnlock()

	if !upgraded || conn == nil {
		return 0, nil, ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set read deadline for next message
	if pongWait > 0 {
		deadline := time.Now().Add(pongWait)
		if err := conn.SetReadDeadline(deadline); err != nil {
			return 0, nil, err
		}
	}

	messageType, p, err = conn.ReadMessage()

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
func (c *fiberWebSocketContext) WriteJSON(v any) error {
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
func (c *fiberWebSocketContext) ReadJSON(v any) error {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	pongWait := c.config.PongWait
	c.mu.RUnlock()

	if !upgraded || conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set read deadline
	if pongWait > 0 {
		deadline := time.Now().Add(pongWait)
		if err := conn.SetReadDeadline(deadline); err != nil {
			return err
		}
	}

	return conn.ReadJSON(v)
}

// Close closes the WebSocket connection
func (c *fiberWebSocketContext) Close() error {
	return c.CloseWithStatus(CloseNormalClosure, "")
}

// CloseWithStatus closes the WebSocket connection with a status code and reason
func (c *fiberWebSocketContext) CloseWithStatus(code int, reason string) error {
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
		_ = conn.Close()
		return err
	}

	if err := conn.WriteMessage(websocket.CloseMessage, message); err != nil {
		// Still close the connection
		_ = conn.Close()
		return err
	}

	// Close the connection
	if err := conn.Close(); err != nil {
		return err
	}

	return nil
}

// SetReadDeadline sets the read deadline for the connection
func (c *fiberWebSocketContext) SetReadDeadline(t time.Time) error {
	c.mu.RLock()
	conn := c.conn
	upgraded := c.isUpgraded
	c.mu.RUnlock()

	if !upgraded || conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	return conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline for the connection
func (c *fiberWebSocketContext) SetWriteDeadline(t time.Time) error {
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
func (c *fiberWebSocketContext) WritePing(data []byte) error {
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
func (c *fiberWebSocketContext) WritePong(data []byte) error {
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
func (c *fiberWebSocketContext) SetPingHandler(handler func(data []byte) error) {
	c.mu.Lock()
	c.pingHandler = handler
	conn := c.conn
	c.mu.Unlock()

	// Update the handler on the connection if already upgraded
	if conn != nil {
		if handler == nil {
			conn.SetPingHandler(nil)
			return
		}
		conn.SetPingHandler(func(appData string) error {
			return handler([]byte(appData))
		})
	}
}

// SetPongHandler sets the pong handler
func (c *fiberWebSocketContext) SetPongHandler(handler func(data []byte) error) {
	c.mu.Lock()
	c.pongHandler = handler
	conn := c.conn
	c.mu.Unlock()

	// Update the handler on the connection if already upgraded
	if conn != nil {
		if handler == nil {
			conn.SetPongHandler(nil)
			return
		}
		conn.SetPongHandler(func(appData string) error {
			return handler([]byte(appData))
		})
	}
}

// SetCloseHandler sets the close handler
func (c *fiberWebSocketContext) SetCloseHandler(handler func(code int, text string) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeHandlers = append(c.closeHandlers, handler)
}

// Subprotocol returns the negotiated subprotocol
func (c *fiberWebSocketContext) Subprotocol() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subprotocol
}

// Extensions returns the negotiated extensions
func (c *fiberWebSocketContext) Extensions() []string {
	return []string{}
}

// RemoteAddr returns the remote address of the connection
func (c *fiberWebSocketContext) RemoteAddr() string {
	if c.fiberContext == nil {
		return ""
	}
	return c.fiberContext.IP()
}

// LocalAddr returns the local address of the connection
func (c *fiberWebSocketContext) LocalAddr() string {
	if c.fiberContext == nil {
		return ""
	}
	if meta := c.fiberContext.getMeta(); meta != nil {
		return fmt.Sprintf("%s:%s", meta.host, meta.port)
	}
	if ctx := c.fiberContext.liveCtx(); ctx != nil {
		return fmt.Sprintf("%s:%s", ctx.Hostname(), ctx.Port())
	}
	return ""
}

// IsConnected returns true if the WebSocket is connected
func (c *fiberWebSocketContext) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isUpgraded && c.conn != nil
}

// ConnectionID returns a unique identifier for this connection
func (c *fiberWebSocketContext) ConnectionID() string {
	return c.connectionID
}

// UpgradeData returns pre-upgrade data, if available
func (c *fiberWebSocketContext) UpgradeData(key string) (any, bool) {
	if c.upgradeData == nil {
		return nil, false
	}
	val, ok := c.upgradeData[key]
	return val, ok
}

func (c *fiberWebSocketContext) Next() error {
	c.index++
	if c.index >= len(c.handlers) {
		return nil
	}
	return c.handlers[c.index].Handler(c)
}

// FiberWebSocketFactory implements WebSocketContextFactory for Fiber
type FiberWebSocketFactory struct {
	logger Logger
}

// NewFiberWebSocketFactory creates a new Fiber WebSocket factory
func NewFiberWebSocketFactory(logger Logger) *FiberWebSocketFactory {
	return &FiberWebSocketFactory{
		logger: logger,
	}
}

// CreateWebSocketContext creates a Fiber specific WebSocket context
// Note: This factory is mainly for testing. In production, use FiberWebSocketHandler
func (f *FiberWebSocketFactory) CreateWebSocketContext(c Context, config WebSocketConfig) (WebSocketContext, error) {
	// Ensure it's a Fiber context
	fiberCtx, ok := c.(*fiberContext)
	if !ok {
		return nil, fmt.Errorf("expected fiberContext, got %T", c)
	}

	// Create WebSocket context (without actual connection for factory pattern)
	wsCtx, err := NewFiberWebSocketContext(fiberCtx.ctx, config, f.logger)
	if err != nil {
		return nil, err
	}

	// Note: Actual WebSocket upgrade should happen through FiberWebSocketHandler
	// This factory method is mainly for interface compatibility and testing
	if err := wsCtx.WebSocketUpgrade(); err != nil {
		return nil, err
	}

	return wsCtx, nil
}

// SupportsWebSocket returns true as Fiber supports WebSockets
func (f *FiberWebSocketFactory) SupportsWebSocket() bool {
	return true
}

// AdapterName returns the adapter name
func (f *FiberWebSocketFactory) AdapterName() string {
	return "fiber"
}

// RegisterFiberWebSocketFactory registers the Fiber WebSocket factory globally
func RegisterFiberWebSocketFactory(logger Logger) {
	if logger == nil {
		logger = &defaultLogger{}
	}
	factory := NewFiberWebSocketFactory(logger)
	RegisterWebSocketFactory("fiber", factory)
}

func init() {
	// Ensure Fiber adapters have a WebSocket factory available without manual registration.
	RegisterFiberWebSocketFactory(nil)
}

// FiberWebSocketHandler creates a Fiber-specific WebSocket handler using contrib/websocket
func FiberWebSocketHandler(config WebSocketConfig, handler func(WebSocketContext) error) fiber.Handler {
	// Apply defaults to config
	config.ApplyDefaults()

	// Return a wrapper function that delegates to WebSocket
	return func(c *fiber.Ctx) error {
		// 1. Handle OnPreUpgrade if configured
		var upgradeData UpgradeData
		if config.OnPreUpgrade != nil {
			// Create a temporary context for the hook
			logger := &defaultLogger{}
			ctx := NewFiberContext(c, logger)

			// Execute the hook
			var err error
			upgradeData, err = config.OnPreUpgrade(ctx)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).SendString(err.Error())
			}
		}

		return runFiberWebSocketUpgrade(c, &defaultLogger{}, nil, config, upgradeData, handler)
	}
}

func handleFiberMiddlewareWebSocketUpgrade(c Context, config WebSocketConfig, upgradeData UpgradeData) error {
	fiberCtx, ok := c.(*fiberContext)
	if !ok || fiberCtx == nil || fiberCtx.ctx == nil {
		return fmt.Errorf("expected fiberContext, got %T", c)
	}

	return runFiberWebSocketUpgrade(fiberCtx.ctx, fiberCtx.logger, fiberCtx, config, upgradeData, func(ws WebSocketContext) error {
		return ws.Next()
	})
}

func buildFiberWebSocketConfig(config WebSocketConfig) websocket.Config {
	wsConfig := websocket.Config{
		ReadBufferSize:  config.ReadBufferSize,
		WriteBufferSize: config.WriteBufferSize,
		Subprotocols:    config.Subprotocols,
		Origins:         config.Origins,
	}

	// Set origin checker
	if config.CheckOrigin != nil {
		wsConfig.Filter = func(c *fiber.Ctx) bool {
			origin := c.Get("Origin")
			return config.CheckOrigin(origin)
		}
	} else {
		wsConfig.Filter = func(c *fiber.Ctx) bool {
			return validateFiberOrigin(c, config.Origins)
		}
	}

	return wsConfig
}

func captureFiberContextForWebSocket(c *fiber.Ctx, logger Logger, prototype *fiberContext) (*fiberContext, context.Context) {
	if logger == nil {
		logger = &defaultLogger{}
	}

	safeCtx := c.UserContext()
	if prototype != nil && prototype.cachedCtx != nil {
		safeCtx = prototype.cachedCtx
	}
	if safeCtx == nil {
		safeCtx = context.Background()
	}

	captured := NewFiberContext(c, logger).(*fiberContext)
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
	return captured, safeCtx
}

func runFiberWebSocketUpgrade(c *fiber.Ctx, logger Logger, prototype *fiberContext, config WebSocketConfig, upgradeData UpgradeData, invoke func(WebSocketContext) error) error {
	if logger == nil {
		logger = &defaultLogger{}
	}

	wsConfig := buildFiberWebSocketConfig(config)
	capturedFiberCtx, safeCtx := captureFiberContextForWebSocket(c, logger, prototype)

	wsHandler := websocket.New(func(conn *websocket.Conn) {
		baseWsCtx := &fiberWebSocketContext{
			fiberContext:  capturedFiberCtx,
			config:        config,
			conn:          conn,
			isUpgraded:    true,
			connectionID:  fmt.Sprintf("fiber-ws-%s-%d", conn.RemoteAddr().String(), time.Now().UnixNano()),
			closeHandlers: make([]func(code int, text string) error, 0),
			upgradeData:   upgradeData,
			userCtx:       safeCtx,
		}

		if config.MaxMessageSize > 0 {
			conn.SetReadLimit(config.MaxMessageSize)
		}

		conn.SetPingHandler(func(appData string) error {
			writeTimeout := baseWsCtx.getWriteTimeout()
			deadline := time.Now().Add(writeTimeout)
			baseWsCtx.writeMu.Lock()
			if err := conn.SetWriteDeadline(deadline); err != nil {
				baseWsCtx.writeMu.Unlock()
				return err
			}
			if err := conn.WriteMessage(websocket.PongMessage, []byte(appData)); err != nil {
				baseWsCtx.writeMu.Unlock()
				return err
			}
			baseWsCtx.writeMu.Unlock()
			baseWsCtx.mu.RLock()
			pingHandler := baseWsCtx.pingHandler
			baseWsCtx.mu.RUnlock()
			if pingHandler != nil {
				return pingHandler([]byte(appData))
			}
			return nil
		})

		conn.SetPongHandler(func(appData string) error {
			if config.PongWait > 0 {
				deadline := time.Now().Add(config.PongWait)
				if err := conn.SetReadDeadline(deadline); err != nil {
					return err
				}
			}
			baseWsCtx.mu.RLock()
			pongHandler := baseWsCtx.pongHandler
			baseWsCtx.mu.RUnlock()
			if pongHandler != nil {
				return pongHandler([]byte(appData))
			}
			return nil
		})

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

		if config.OnConnect != nil {
			if err := config.OnConnect(baseWsCtx); err != nil {
				logger.Info("OnConnect handler error", "error", err)
				_ = conn.Close()
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
func (c *fiberWebSocketContext) getWriteTimeout() time.Duration {
	if c.config.WriteTimeout > 0 {
		return c.config.WriteTimeout
	}
	return 10 * time.Second // Default
}

// RouteName returns the route name from context
func (c *fiberWebSocketContext) RouteName() string {
	if name, ok := RouteNameFromContext(c.Context()); ok {
		return name
	}
	return ""
}

// RouteParams returns all route parameters as a map
func (c *fiberWebSocketContext) RouteParams() map[string]string {
	if params, ok := RouteParamsFromContext(c.Context()); ok {
		return params
	}
	return make(map[string]string)
}

// validateFiberOrigin validates the origin for Fiber WebSocket requests
func validateFiberOrigin(c *fiber.Ctx, allowedOrigins []string) bool {
	origin := strings.TrimSpace(c.Get("Origin"))
	if len(allowedOrigins) == 0 {
		if origin == "" {
			return true
		}
		host := c.Hostname()
		if port := c.Port(); port != "" {
			host = host + ":" + port
		}
		return originMatchesRequest(origin, requestScheme(NewFiberContext(c, nil)), host)
	}
	return matchesAnyOriginPattern(origin, allowedOrigins)
}
