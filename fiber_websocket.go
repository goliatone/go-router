package router

import (
	"fmt"
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
	connectionID   string
	subprotocol    string
	closeHandlers  []func(code int, text string) error
	pingHandler    func(data []byte) error
	pongHandler    func(data []byte) error
	messageHandler func(messageType int, data []byte) error
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
	}, nil
}

// IsWebSocket returns true if the connection has been upgraded to WebSocket
func (c *fiberWebSocketContext) IsWebSocket() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isUpgraded
}

// WebSocketUpgrade upgrades the HTTP connection to WebSocket
func (c *fiberWebSocketContext) WebSocketUpgrade() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isUpgraded {
		return nil // Already upgraded
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
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set write deadline
	writeTimeout := c.getWriteTimeout()
	if writeTimeout > 0 {
		deadline := time.Now().Add(writeTimeout)
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}

	return c.conn.WriteMessage(messageType, data)
}

// ReadMessage reads a message from the WebSocket connection
func (c *fiberWebSocketContext) ReadMessage() (messageType int, p []byte, err error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return 0, nil, ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set read deadline for next message
	if c.config.PongWait > 0 {
		deadline := time.Now().Add(c.config.PongWait)
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return 0, nil, err
		}
	}

	messageType, p, err = c.conn.ReadMessage()

	// Call message handler if set
	if err == nil && c.messageHandler != nil {
		if handlerErr := c.messageHandler(messageType, p); handlerErr != nil {
			// Log error but don't fail the read
			c.logger.Info("Message handler error", "error", handlerErr)
		}
	}

	// Call OnMessage handler if configured
	if err == nil && c.config.OnMessage != nil {
		if handlerErr := c.config.OnMessage(c, messageType, p); handlerErr != nil {
			// Log error but don't fail the read
			c.logger.Info("OnMessage handler error", "error", handlerErr)
		}
	}

	return messageType, p, err
}

// WriteJSON sends a JSON message
func (c *fiberWebSocketContext) WriteJSON(v any) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set write deadline
	writeTimeout := c.getWriteTimeout()
	if writeTimeout > 0 {
		deadline := time.Now().Add(writeTimeout)
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}

	return c.conn.WriteJSON(v)
}

// ReadJSON reads a JSON message
func (c *fiberWebSocketContext) ReadJSON(v any) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set read deadline
	if c.config.PongWait > 0 {
		deadline := time.Now().Add(c.config.PongWait)
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return err
		}
	}

	return c.conn.ReadJSON(v)
}

// Close closes the WebSocket connection
func (c *fiberWebSocketContext) Close() error {
	return c.CloseWithStatus(CloseNormalClosure, "")
}

// CloseWithStatus closes the WebSocket connection with a status code and reason
func (c *fiberWebSocketContext) CloseWithStatus(code int, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isUpgraded || c.conn == nil {
		return nil // Not connected
	}

	// Call OnDisconnect handler if configured
	if c.config.OnDisconnect != nil {
		c.config.OnDisconnect(c, nil)
	}

	// Call close handlers
	for _, handler := range c.closeHandlers {
		if err := handler(code, reason); err != nil {
			c.logger.Info("Close handler error", "error", err)
		}
	}

	// Send close message
	message := websocket.FormatCloseMessage(code, reason)
	writeTimeout := c.getWriteTimeout()
	deadline := time.Now().Add(writeTimeout)

	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		// Still try to close the connection
		c.conn.Close()
		c.isUpgraded = false
		c.conn = nil
		return err
	}

	if err := c.conn.WriteMessage(websocket.CloseMessage, message); err != nil {
		// Still close the connection
		c.conn.Close()
		c.isUpgraded = false
		c.conn = nil
		return err
	}

	// Close the connection
	if err := c.conn.Close(); err != nil {
		c.isUpgraded = false
		c.conn = nil
		return err
	}

	c.isUpgraded = false
	c.conn = nil

	return nil
}

// SetReadDeadline sets the read deadline for the connection
func (c *fiberWebSocketContext) SetReadDeadline(t time.Time) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline for the connection
func (c *fiberWebSocketContext) SetWriteDeadline(t time.Time) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	return c.conn.SetWriteDeadline(t)
}

// WritePing sends a ping message
func (c *fiberWebSocketContext) WritePing(data []byte) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set write deadline
	writeTimeout := c.getWriteTimeout()
	if writeTimeout > 0 {
		deadline := time.Now().Add(writeTimeout)
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}

	return c.conn.WriteMessage(websocket.PingMessage, data)
}

// WritePong sends a pong message
func (c *fiberWebSocketContext) WritePong(data []byte) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	// Set write deadline
	writeTimeout := c.getWriteTimeout()
	if writeTimeout > 0 {
		deadline := time.Now().Add(writeTimeout)
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}

	return c.conn.WriteMessage(websocket.PongMessage, data)
}

// SetPingHandler sets the ping handler
func (c *fiberWebSocketContext) SetPingHandler(handler func(data []byte) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pingHandler = handler

	// Update the handler on the connection if already upgraded
	if c.conn != nil {
		c.conn.SetPingHandler(func(appData string) error {
			return handler([]byte(appData))
		})
	}
}

// SetPongHandler sets the pong handler
func (c *fiberWebSocketContext) SetPongHandler(handler func(data []byte) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pongHandler = handler

	// Update the handler on the connection if already upgraded
	if c.conn != nil {
		c.conn.SetPongHandler(func(appData string) error {
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
	return c.ctx.IP()
}

// LocalAddr returns the local address of the connection
func (c *fiberWebSocketContext) LocalAddr() string {
	return fmt.Sprintf("%s:%s", c.ctx.Hostname(), c.ctx.Port())
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
// Direct adapter contexts don't have upgrade data - use the WebSocket middleware for that
func (c *fiberWebSocketContext) UpgradeData(key string) (any, bool) {
	return nil, false
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
		// Create websocket upgrader config
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
		} else if len(config.Origins) > 0 {
			wsConfig.Filter = func(c *fiber.Ctx) bool {
				return validateFiberOrigin(c, config.Origins)
			}
		}

		// Create WebSocket handler with access to upgradeData via closure
		wsHandler := createFiberWSHandler(config, handler, nil)

		// Execute the WebSocket handler
		return wsHandler(c)
	}
}

// createFiberWSHandler creates the actual WebSocket handler function
func createFiberWSHandler(config WebSocketConfig, handler func(WebSocketContext) error, upgradeData UpgradeData) fiber.Handler {
	// Create websocket upgrader config (duplicated from above for clarity)
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
	} else if len(config.Origins) > 0 {
		wsConfig.Filter = func(c *fiber.Ctx) bool {
			return validateFiberOrigin(c, config.Origins)
		}
	}

	// Return a handler that captures the fiber context during upgrade
	return func(c *fiber.Ctx) error {
		// Capture the fiber context before the upgrade
		logger := &defaultLogger{} // Use default logger if not available
		capturedFiberCtx := NewFiberContext(c, logger).(*fiberContext)

		wsHandler := websocket.New(func(conn *websocket.Conn) {
			// Create base WebSocket context with properly initialized fiber context
			baseWsCtx := &fiberWebSocketContext{
				fiberContext:  capturedFiberCtx, // Use the captured context instead of nil
				config:        config,
				conn:          conn,
				isUpgraded:    true,
				connectionID:  fmt.Sprintf("fiber-ws-%s-%d", conn.RemoteAddr().String(), time.Now().UnixNano()),
				closeHandlers: make([]func(code int, text string) error, 0),
			}

			// Create enhanced WebSocket context
			wsCtx := &enhancedWebSocketContext{
				WebSocketContext: baseWsCtx,
				upgradeData:      nil, // Upgrade data is handled by middleware
			}

			if config.MaxMessageSize > 0 {
				conn.SetReadLimit(config.MaxMessageSize)
			}

			conn.SetPingHandler(func(appData string) error {
				writeTimeout := baseWsCtx.getWriteTimeout()
				deadline := time.Now().Add(writeTimeout)
				if err := conn.SetWriteDeadline(deadline); err != nil {
					return err
				}
				if err := conn.WriteMessage(websocket.PongMessage, []byte(appData)); err != nil {
					return err
				}
				if baseWsCtx.pingHandler != nil {
					return baseWsCtx.pingHandler([]byte(appData))
				}
				return nil
			})

			conn.SetPongHandler(func(appData string) error {
				// Update read deadline on pong receipt
				if config.PongWait > 0 {
					deadline := time.Now().Add(config.PongWait)
					if err := conn.SetReadDeadline(deadline); err != nil {
						return err
					}
				}
				// Call custom handler if set
				if baseWsCtx.pongHandler != nil {
					return baseWsCtx.pongHandler([]byte(appData))
				}
				return nil
			})

			conn.SetCloseHandler(func(code int, text string) error {
				// Call all registered close handlers
				for _, closeHandler := range baseWsCtx.closeHandlers {
					if err := closeHandler(code, text); err != nil {
						// Log error but continue with other handlers
						logger.Info("Close handler error", "error", err)
					}
				}
				return nil
			})

			// Store negotiated subprotocol
			baseWsCtx.subprotocol = conn.Subprotocol()

			// Call the main handler
			if err := handler(wsCtx); err != nil {
				logger.Info("WebSocket handler error", "error", err)
			}
		}, wsConfig)

		// Execute the WebSocket handler
		return wsHandler(c)
	}
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
	origin := c.Get("Origin")

	// No origin restrictions if list is empty
	if len(allowedOrigins) == 0 {
		return true
	}

	// Check each allowed origin
	for _, allowed := range allowedOrigins {
		if allowed == "*" {
			return true
		}
		if allowed == origin {
			return true
		}
		// Support wildcard subdomains
		if len(allowed) > 2 && allowed[0] == '*' && allowed[1] == '.' {
			domain := allowed[2:]
			if len(origin) > len(domain) {
				if origin[len(origin)-len(domain):] == domain {
					return true
				}
			}
		}
	}

	return false
}
