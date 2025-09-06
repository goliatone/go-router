package router

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	connectionID   string
	subprotocol    string
	closeHandlers  []func(code int, text string) error
	pingHandler    func(appData string) error
	pongHandler    func(appData string) error
	messageHandler func(messageType int, data []byte) error
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
			// Default: check against configured origins
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
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
		if err := conn.WriteMessage(websocket.PongMessage, []byte(appData)); err != nil {
			return err
		}
		// Call custom handler if set
		if c.pingHandler != nil {
			return c.pingHandler(appData)
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
		if c.pongHandler != nil {
			return c.pongHandler(appData)
		}
		return nil
	})

	// Set close handler
	conn.SetCloseHandler(func(code int, text string) error {
		// Call all registered close handlers
		for _, handler := range c.closeHandlers {
			if err := handler(code, text); err != nil {
				// Log error but continue with other handlers
				fmt.Printf("Close handler error: %v\n", err)
			}
		}
		return nil
	})

	// Store negotiated subprotocol
	c.subprotocol = conn.Subprotocol()

	// Call OnConnect handler if configured
	if c.config.OnConnect != nil {
		if err := c.config.OnConnect(c); err != nil {
			conn.Close()
			return fmt.Errorf("OnConnect handler failed: %w", err)
		}
	}

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
func (c *httpRouterWebSocketContext) ReadMessage() (messageType int, p []byte, err error) {
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
			fmt.Printf("Message handler error: %v\n", handlerErr)
		}
	}

	// Call OnMessage handler if configured
	if err == nil && c.config.OnMessage != nil {
		if handlerErr := c.config.OnMessage(c, messageType, p); handlerErr != nil {
			// Log error but don't fail the read
			fmt.Printf("OnMessage handler error: %v\n", handlerErr)
		}
	}

	return messageType, p, err
}

// WriteJSON sends a JSON message
func (c *httpRouterWebSocketContext) WriteJSON(v any) error {
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
func (c *httpRouterWebSocketContext) ReadJSON(v any) error {
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
func (c *httpRouterWebSocketContext) Close() error {
	return c.CloseWithStatus(websocket.CloseNormalClosure, "")
}

// CloseWithStatus closes the WebSocket connection with a status code and reason
func (c *httpRouterWebSocketContext) CloseWithStatus(code int, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isUpgraded || c.conn == nil {
		return nil // Not connected
	}

	// Call OnDisconnect handler if configured
	if c.config.OnDisconnect != nil {
		c.config.OnDisconnect(c, nil)
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
func (c *httpRouterWebSocketContext) SetReadDeadline(t time.Time) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline for the connection
func (c *httpRouterWebSocketContext) SetWriteDeadline(t time.Time) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isUpgraded || c.conn == nil {
		return ErrWebSocketUpgradeFailed(fmt.Errorf("connection not upgraded"))
	}

	return c.conn.SetWriteDeadline(t)
}

// WritePing sends a ping message
func (c *httpRouterWebSocketContext) WritePing(data []byte) error {
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
func (c *httpRouterWebSocketContext) WritePong(data []byte) error {
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
func (c *httpRouterWebSocketContext) SetPingHandler(handler func(data []byte) error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pingHandler = func(appData string) error {
		return handler([]byte(appData))
	}

	// Update the handler on the connection if already upgraded
	if c.conn != nil {
		c.conn.SetPingHandler(c.pingHandler)
	}
}

// SetPongHandler sets the pong handler
func (c *httpRouterWebSocketContext) SetPongHandler(handler func(data []byte) error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pongHandler = func(appData string) error {
		return handler([]byte(appData))
	}

	// Update the handler on the connection if already upgraded
	if c.conn != nil {
		c.conn.SetPongHandler(c.pongHandler)
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

// Helper function to validate origin for HTTP requests
func validateHTTPOrigin(r *http.Request, allowedOrigins []string) bool {
	origin := r.Header.Get("Origin")

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

// HTTPRouterWebSocketFactory implements WebSocketContextFactory for HTTPRouter
type HTTPRouterWebSocketFactory struct {
	views Views
}

// NewHTTPRouterWebSocketFactory creates a new HTTPRouter WebSocket factory
func NewHTTPRouterWebSocketFactory(views Views) *HTTPRouterWebSocketFactory {
	return &HTTPRouterWebSocketFactory{
		views: views,
	}
}

// CreateWebSocketContext creates an HTTPRouter-specific WebSocket context
func (f *HTTPRouterWebSocketFactory) CreateWebSocketContext(c Context, config WebSocketConfig) (WebSocketContext, error) {
	// Ensure it's an HTTPRouter context
	httpCtx, ok := c.(*httpRouterContext)
	if !ok {
		return nil, fmt.Errorf("expected httpRouterContext, got %T", c)
	}

	// Create WebSocket context
	wsCtx, err := NewHTTPRouterWebSocketContext(httpCtx.w, httpCtx.r, httpCtx.params, config, f.views)
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

// HTTPRouterWebSocketHandler creates an HTTPRouter-specific WebSocket handler
func HTTPRouterWebSocketHandler(config WebSocketConfig, handler func(WebSocketContext) error, views Views) httprouter.Handle {
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

		// Perform the upgrade
		if err := wsCtx.WebSocketUpgrade(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
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
