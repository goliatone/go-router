package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
)

// httpRouterWebSocketContext implements WebSocketContext for HTTPRouter
type httpRouterWebSocketContext struct {
	*httpRouterContext
	conn           *websocket.Conn
	config         WebSocketConfig
	upgrader       *websocket.Upgrader
	isUpgraded     bool
	mu             sync.RWMutex
	writeMu        sync.Mutex
	connectionID   string
	subprotocol    string
	closeHandlers  []func(code int, text string) error
	pingHandler    func(appData string) error
	pongHandler    func(appData string) error
	messageHandler func(messageType int, data []byte) error
	upgradeData    UpgradeData
}

// Ensure httpRouterWebSocketContext implements WebSocketContext
var _ WebSocketContext = (*httpRouterWebSocketContext)(nil)

// NewHTTPRouterWebSocketContext creates a new HTTPRouter WebSocket context
func NewHTTPRouterWebSocketContext(w http.ResponseWriter, r *http.Request, ps httprouter.Params, config WebSocketConfig, views Views) (*httpRouterWebSocketContext, error) {
	// Create base HTTPRouter context
	baseCtx := newHTTPRouterContext(w, r, ps, views)

	// Generate connection ID
	connID := fmt.Sprintf("httprouter-ws-%s-%d", r.RemoteAddr, time.Now().UnixNano())

	// Create upgrader with configuration
	upgrader := &websocket.Upgrader{
		ReadBufferSize:    config.ReadBufferSize,
		WriteBufferSize:   config.WriteBufferSize,
		HandshakeTimeout:  config.HandshakeTimeout,
		Subprotocols:      config.Subprotocols,
		EnableCompression: config.EnableCompression,
		CheckOrigin: func(r *http.Request) bool {
			if config.CheckOrigin != nil {
				origin := r.Header.Get("Origin")
				return config.CheckOrigin(origin)
			}
			return validateHTTPOrigin(r, config.Origins)
		},
	}

	return &httpRouterWebSocketContext{
		httpRouterContext: baseCtx,
		config:            config,
		upgrader:          upgrader,
		connectionID:      connID,
		closeHandlers:     make([]func(code int, text string) error, 0),
	}, nil
}

// IsWebSocket returns true if the connection has been upgraded to WebSocket
func (c *httpRouterWebSocketContext) IsWebSocket() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isUpgraded
}

// WebSocketUpgrade upgrades the HTTP connection to WebSocket
func (c *httpRouterWebSocketContext) WebSocketUpgrade() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isUpgraded {
		return nil // Already upgraded
	}

	// Perform the upgrade
	conn, err := c.upgrader.Upgrade(c.w, c.r, nil)
	if err != nil {
		return ErrWebSocketUpgradeFailed(err)
	}

	c.conn = conn
	c.isUpgraded = true

	// Set connection parameters
	if c.config.MaxMessageSize > 0 {
		conn.SetReadLimit(c.config.MaxMessageSize)
	}

	// Set ping handler
	conn.SetPingHandler(func(appData string) error {
		// Send pong response
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
		// Call custom handler if set
		c.mu.RLock()
		pingHandler := c.pingHandler
		c.mu.RUnlock()
		if pingHandler != nil {
			return pingHandler(appData)
		}
		return nil
	})

	// Set pong handler
	conn.SetPongHandler(func(appData string) error {
		// Update read deadline on pong receipt
		if c.config.PongWait > 0 {
			deadline := time.Now().Add(c.config.PongWait)
			if err := conn.SetReadDeadline(deadline); err != nil {
				return err
			}
		}
		// Call custom handler if set
		c.mu.RLock()
		pongHandler := c.pongHandler
		c.mu.RUnlock()
		if pongHandler != nil {
			return pongHandler(appData)
		}
		return nil
	})

	// Set close handler
	conn.SetCloseHandler(func(code int, text string) error {
		// Call all registered close handlers
		c.mu.RLock()
		handlers := append([]func(code int, text string) error{}, c.closeHandlers...)
		c.mu.RUnlock()
		for _, handler := range handlers {
			if err := handler(code, text); err != nil {
				// Log error but continue with other handlers
				fmt.Printf("Close handler error: %v\n", err)
			}
		}
		return nil
	})

	// Store negotiated subprotocol
	c.subprotocol = conn.Subprotocol()

	return nil
}

// getWriteTimeout returns the write timeout to use
func (c *httpRouterWebSocketContext) getWriteTimeout() time.Duration {
	if c.config.WriteTimeout > 0 {
		return c.config.WriteTimeout
	}
	return 10 * time.Second // Default
}

// WriteMessage sends a message to the WebSocket connection
func (c *httpRouterWebSocketContext) WriteMessage(messageType int, data []byte) error {
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
func (c *httpRouterWebSocketContext) ReadMessage() (messageType int, p []byte, err error) {
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
			fmt.Printf("Message handler error: %v\n", handlerErr)
		}
	}

	// Call OnMessage handler if configured
	if err == nil && onMessage != nil {
		if handlerErr := onMessage(c, messageType, p); handlerErr != nil {
			// Log error but don't fail the read
			fmt.Printf("OnMessage handler error: %v\n", handlerErr)
		}
	}

	return messageType, p, err
}

// WriteJSON sends a JSON message
func (c *httpRouterWebSocketContext) WriteJSON(v any) error {
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
func (c *httpRouterWebSocketContext) ReadJSON(v any) error {
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
func (c *httpRouterWebSocketContext) Close() error {
	return c.CloseWithStatus(websocket.CloseNormalClosure, "")
}

// CloseWithStatus closes the WebSocket connection with a status code and reason
func (c *httpRouterWebSocketContext) CloseWithStatus(code int, reason string) error {
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

	for _, handler := range closeHandlers {
		if err := handler(code, reason); err != nil {
			fmt.Printf("Close handler error: %v\n", err)
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
func (c *httpRouterWebSocketContext) SetReadDeadline(t time.Time) error {
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
func (c *httpRouterWebSocketContext) SetWriteDeadline(t time.Time) error {
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
func (c *httpRouterWebSocketContext) WritePing(data []byte) error {
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
func (c *httpRouterWebSocketContext) WritePong(data []byte) error {
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
func (c *httpRouterWebSocketContext) SetPingHandler(handler func(data []byte) error) {
	c.mu.Lock()
	if handler == nil {
		c.pingHandler = nil
	} else {
		c.pingHandler = func(appData string) error {
			return handler([]byte(appData))
		}
	}
	conn := c.conn
	pingHandler := c.pingHandler
	c.mu.Unlock()

	// Update the handler on the connection if already upgraded
	if conn != nil {
		conn.SetPingHandler(pingHandler)
	}
}

// SetPongHandler sets the pong handler
func (c *httpRouterWebSocketContext) SetPongHandler(handler func(data []byte) error) {
	c.mu.Lock()
	if handler == nil {
		c.pongHandler = nil
	} else {
		c.pongHandler = func(appData string) error {
			return handler([]byte(appData))
		}
	}
	conn := c.conn
	pongHandler := c.pongHandler
	c.mu.Unlock()

	// Update the handler on the connection if already upgraded
	if conn != nil {
		conn.SetPongHandler(pongHandler)
	}
}

// SetCloseHandler sets the close handler
func (c *httpRouterWebSocketContext) SetCloseHandler(handler func(code int, text string) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeHandlers = append(c.closeHandlers, handler)
}

// Subprotocol returns the negotiated subprotocol
func (c *httpRouterWebSocketContext) Subprotocol() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subprotocol
}

// Extensions returns the negotiated extensions
func (c *httpRouterWebSocketContext) Extensions() []string {
	// Gorilla websocket doesn't expose extensions easily
	// Return empty slice for now
	return []string{}
}

// RemoteAddr returns the remote address of the connection
func (c *httpRouterWebSocketContext) RemoteAddr() string {
	if c.conn != nil {
		return c.conn.RemoteAddr().String()
	}
	return c.r.RemoteAddr
}

// LocalAddr returns the local address of the connection
func (c *httpRouterWebSocketContext) LocalAddr() string {
	if c.conn != nil {
		return c.conn.LocalAddr().String()
	}
	// For HTTP request, construct from host header
	return c.r.Host
}

// IsConnected returns true if the WebSocket is connected
func (c *httpRouterWebSocketContext) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isUpgraded && c.conn != nil
}

// ConnectionID returns a unique identifier for this connection
func (c *httpRouterWebSocketContext) ConnectionID() string {
	return c.connectionID
}

// UpgradeData returns pre-upgrade data, if available
func (c *httpRouterWebSocketContext) UpgradeData(key string) (any, bool) {
	if c.upgradeData == nil {
		return nil, false
	}
	val, ok := c.upgradeData[key]
	return val, ok
}

// RouteName returns the route name from context
func (c *httpRouterWebSocketContext) RouteName() string {
	if name, ok := RouteNameFromContext(c.Context()); ok {
		return name
	}
	return ""
}

// RouteParams returns all route parameters as a map
func (c *httpRouterWebSocketContext) RouteParams() map[string]string {
	if params, ok := RouteParamsFromContext(c.Context()); ok {
		return params
	}
	return make(map[string]string)
}

func (c *httpRouterWebSocketContext) Next() error {
	c.index++
	if c.index >= len(c.handlers) {
		return nil
	}
	return c.handlers[c.index].Handler(c)
}

// Helper function to validate origin for HTTP requests
func validateHTTPOrigin(r *http.Request, allowedOrigins []string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if len(allowedOrigins) == 0 {
		if origin == "" {
			return true
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		return originMatchesRequest(origin, scheme, r.Host)
	}
	return matchesAnyOriginPattern(origin, allowedOrigins)
}

// HTTPRouterWebSocketFactory implements WebSocketContextFactory for HTTPRouter
type HTTPRouterWebSocketFactory struct{}

// NewHTTPRouterWebSocketFactory creates a new HTTPRouter WebSocket factory
func NewHTTPRouterWebSocketFactory(_ Views) *HTTPRouterWebSocketFactory {
	return &HTTPRouterWebSocketFactory{}
}

// CreateWebSocketContext creates an HTTPRouter-specific WebSocket context
func (f *HTTPRouterWebSocketFactory) CreateWebSocketContext(c Context, config WebSocketConfig) (WebSocketContext, error) {
	// Ensure it's an HTTPRouter context
	httpCtx, ok := c.(*httpRouterContext)
	if !ok {
		return nil, fmt.Errorf("expected httpRouterContext, got %T", c)
	}

	// Create WebSocket context using the views already attached to the HTTP context
	wsCtx, err := NewHTTPRouterWebSocketContext(httpCtx.w, httpCtx.r, httpCtx.params, config, httpCtx.views)
	if err != nil {
		return nil, err
	}

	// Perform the upgrade
	if err := wsCtx.WebSocketUpgrade(); err != nil {
		return nil, err
	}

	return wsCtx, nil
}

// SupportsWebSocket returns true as HTTPRouter supports WebSockets
func (f *HTTPRouterWebSocketFactory) SupportsWebSocket() bool {
	return true
}

// AdapterName returns the adapter name
func (f *HTTPRouterWebSocketFactory) AdapterName() string {
	return "httprouter"
}

// RegisterHTTPRouterWebSocketFactory registers the HTTPRouter WebSocket factory globally
func RegisterHTTPRouterWebSocketFactory(views Views) {
	factory := NewHTTPRouterWebSocketFactory(views)
	RegisterWebSocketFactory("httprouter", factory)
}

func init() {
	// Ensure the HTTPRouter factory is always available so middleware-based upgrades work out of the box.
	RegisterHTTPRouterWebSocketFactory(nil)
}

// HTTPRouterWebSocketHandler creates an HTTPRouter-specific WebSocket handler
func HTTPRouterWebSocketHandler(config WebSocketConfig, handler func(WebSocketContext) error, views Views) httprouter.Handle {
	// Apply defaults to config
	config.ApplyDefaults()

	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		// Check if it's a WebSocket request
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "WebSocket upgrade required", http.StatusBadRequest)
			return
		}

		// Create WebSocket context
		wsCtx, err := NewHTTPRouterWebSocketContext(w, r, ps, config, views)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Handle OnPreUpgrade if configured
		if config.OnPreUpgrade != nil {
			data, err := config.OnPreUpgrade(wsCtx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			wsCtx.upgradeData = data
		}

		// Perform the upgrade
		if err := wsCtx.WebSocketUpgrade(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Call OnConnect if configured
		if config.OnConnect != nil {
			if err := config.OnConnect(wsCtx); err != nil {
				// Log error (connection already upgraded, can't send HTTP error)
				fmt.Printf("OnConnect handler error: %v\n", err)
				wsCtx.Close()
				return
			}
		}

		// Call the handler
		if err := handler(wsCtx); err != nil {
			// Log error but don't send HTTP response (connection is upgraded)
			fmt.Printf("WebSocket handler error: %v\n", err)
		}
	}
}

// PingPongManager manages ping/pong for HTTPRouter WebSocket connections
type PingPongManager struct {
	pingPeriod time.Duration
	pongWait   time.Duration
	conn       *websocket.Conn
	stopChan   chan bool
	mu         sync.Mutex
}

// NewPingPongManager creates a new ping/pong manager
func NewPingPongManager(conn *websocket.Conn, pingPeriod, pongWait time.Duration) *PingPongManager {
	return &PingPongManager{
		pingPeriod: pingPeriod,
		pongWait:   pongWait,
		conn:       conn,
		stopChan:   make(chan bool),
	}
}

// Start starts the ping/pong loop
func (m *PingPongManager) Start() {
	ticker := time.NewTicker(m.pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			if m.conn == nil {
				m.mu.Unlock()
				return
			}

			if err := m.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				m.mu.Unlock()
				return
			}

			if err := m.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()

		case <-m.stopChan:
			return
		}
	}
}

// Stop stops the ping/pong loop
func (m *PingPongManager) Stop() {
	close(m.stopChan)
	m.mu.Lock()
	m.conn = nil
	m.mu.Unlock()
}

// WriteTextMessage is a helper to write text messages
func WriteTextMessage(conn *websocket.Conn, message string, timeout time.Duration) error {
	if timeout > 0 {
		if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			return err
		}
	}
	return conn.WriteMessage(websocket.TextMessage, []byte(message))
}

// WriteBinaryMessage is a helper to write binary messages
func WriteBinaryMessage(conn *websocket.Conn, data []byte, timeout time.Duration) error {
	if timeout > 0 {
		if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			return err
		}
	}
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

// WriteJSONMessage is a helper to write JSON messages
func WriteJSONMessage(conn *websocket.Conn, v any, timeout time.Duration) error {
	if timeout > 0 {
		if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			return err
		}
	}

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}
