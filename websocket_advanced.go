package router

import (
	"compress/flate"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ============================================================================
// Task 5.1: JSON Message Helpers
// ============================================================================

// JSONMessage represents a standard JSON message structure
type JSONMessage struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// JSONMessageHandler handles JSON messages
type JSONMessageHandler func(ctx WebSocketContext, msg *JSONMessage) error

// WriteJSONSafe writes a JSON message with size validation
func WriteJSONSafe(ctx WebSocketContext, v interface{}, maxSize int64) error {
	// Marshal to JSON first to check size
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json marshal error: %w", err)
	}

	// Check size limit
	if maxSize > 0 && int64(len(data)) > maxSize {
		return ErrWebSocketMessageTooBig(int64(len(data)), maxSize)
	}

	// Send as text message
	return ctx.WriteMessage(TextMessage, data)
}

// ReadJSONSafe reads a JSON message with size validation
func ReadJSONSafe(ctx WebSocketContext, v interface{}, maxSize int64) error {
	messageType, data, err := ctx.ReadMessage()
	if err != nil {
		return err
	}

	// Check if it's a text message
	if messageType != TextMessage {
		return fmt.Errorf("expected text message, got type %d", messageType)
	}

	// Check size limit
	if maxSize > 0 && int64(len(data)) > maxSize {
		return ErrWebSocketMessageTooBig(int64(len(data)), maxSize)
	}

	// Unmarshal JSON
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("json unmarshal error: %w", err)
	}

	return nil
}

// JSONMessageRouter routes JSON messages based on type
type JSONMessageRouter struct {
	handlers map[string]JSONMessageHandler
	mu       sync.RWMutex
	maxSize  int64
}

// NewJSONMessageRouter creates a new JSON message router
func NewJSONMessageRouter(maxSize int64) *JSONMessageRouter {
	return &JSONMessageRouter{
		handlers: make(map[string]JSONMessageHandler),
		maxSize:  maxSize,
	}
}

// Register registers a handler for a message type
func (r *JSONMessageRouter) Register(msgType string, handler JSONMessageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[msgType] = handler
}

// Route routes a message to the appropriate handler
func (r *JSONMessageRouter) Route(ctx WebSocketContext) error {
	var msg JSONMessage
	if err := ReadJSONSafe(ctx, &msg, r.maxSize); err != nil {
		return err
	}

	r.mu.RLock()
	handler, ok := r.handlers[msg.Type]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no handler for message type: %s", msg.Type)
	}

	return handler(ctx, &msg)
}

// ============================================================================
// Task 5.2: Connection Deadline Management
// ============================================================================

// DeadlineManager manages connection deadlines and health checks
type DeadlineManager struct {
	ctx           WebSocketContext
	pingPeriod    time.Duration
	pongWait      time.Duration
	writeWait     time.Duration
	maxPingErrors int
	pingErrors    int
	ticker        *time.Ticker
	done          chan bool
	mu            sync.Mutex
}

// NewDeadlineManager creates a new deadline manager
func NewDeadlineManager(ctx WebSocketContext, config WebSocketConfig) *DeadlineManager {
	pongWait := config.PongWait
	if pongWait == 0 {
		pongWait = 60 * time.Second
	}

	pingPeriod := config.PingPeriod
	if pingPeriod == 0 {
		pingPeriod = (pongWait * 9) / 10
	}

	writeWait := config.WriteTimeout
	if writeWait == 0 {
		writeWait = 10 * time.Second
	}

	return &DeadlineManager{
		ctx:           ctx,
		pingPeriod:    pingPeriod,
		pongWait:      pongWait,
		writeWait:     writeWait,
		maxPingErrors: 3,
		done:          make(chan bool),
	}
}

// Start starts the deadline management
func (d *DeadlineManager) Start() {
	// Set initial read deadline
	d.ctx.SetReadDeadline(time.Now().Add(d.pongWait))

	// Set pong handler to reset read deadline
	d.ctx.SetPongHandler(func(data []byte) error {
		d.mu.Lock()
		d.pingErrors = 0 // Reset error count on successful pong
		d.mu.Unlock()
		return d.ctx.SetReadDeadline(time.Now().Add(d.pongWait))
	})

	// Start ping ticker
	d.ticker = time.NewTicker(d.pingPeriod)
	go d.pingLoop()
}

// Stop stops the deadline management
func (d *DeadlineManager) Stop() {
	if d.ticker != nil {
		d.ticker.Stop()
	}
	close(d.done)
}

// pingLoop sends periodic pings
func (d *DeadlineManager) pingLoop() {
	for {
		select {
		case <-d.ticker.C:
			d.mu.Lock()
			if d.pingErrors >= d.maxPingErrors {
				d.mu.Unlock()
				d.ctx.CloseWithStatus(CloseGoingAway, "ping timeout")
				return
			}
			d.mu.Unlock()

			if err := d.sendPing(); err != nil {
				d.mu.Lock()
				d.pingErrors++
				d.mu.Unlock()
			}

		case <-d.done:
			return
		}
	}
}

// sendPing sends a ping message
func (d *DeadlineManager) sendPing() error {
	d.ctx.SetWriteDeadline(time.Now().Add(d.writeWait))
	return d.ctx.WritePing([]byte(fmt.Sprintf("%d", time.Now().Unix())))
}

// HealthCheck performs a health check on the connection
func (d *DeadlineManager) HealthCheck() error {
	if !d.ctx.IsConnected() {
		return fmt.Errorf("connection not established")
	}

	d.mu.Lock()
	errors := d.pingErrors
	d.mu.Unlock()

	if errors >= d.maxPingErrors {
		return fmt.Errorf("too many ping errors: %d", errors)
	}

	return nil
}

// ============================================================================
// Task 5.3: Subprotocol Negotiation
// ============================================================================

// SubprotocolNegotiator handles subprotocol negotiation
type SubprotocolNegotiator struct {
	supported map[string]SubprotocolHandler
	mu        sync.RWMutex
}

// SubprotocolHandler handles protocol-specific features
type SubprotocolHandler struct {
	Name       string
	Version    string
	Validate   func(ctx WebSocketContext) error
	Initialize func(ctx WebSocketContext) error
	OnMessage  func(ctx WebSocketContext, msgType int, data []byte) error
	OnClose    func(ctx WebSocketContext) error
}

// NewSubprotocolNegotiator creates a new subprotocol negotiator
func NewSubprotocolNegotiator() *SubprotocolNegotiator {
	return &SubprotocolNegotiator{
		supported: make(map[string]SubprotocolHandler),
	}
}

// Register registers a subprotocol handler
func (n *SubprotocolNegotiator) Register(handler SubprotocolHandler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.supported[handler.Name] = handler
}

// GetSupportedProtocols returns the list of supported protocols
func (n *SubprotocolNegotiator) GetSupportedProtocols() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	protocols := make([]string, 0, len(n.supported))
	for name := range n.supported {
		protocols = append(protocols, name)
	}
	return protocols
}

// NegotiateProtocol selects the best matching protocol
func (n *SubprotocolNegotiator) NegotiateProtocol(requested []string) (string, *SubprotocolHandler) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// First matching protocol wins
	for _, req := range requested {
		if handler, ok := n.supported[req]; ok {
			return req, &handler
		}
	}

	return "", nil
}

// HandleConnection handles a connection with the negotiated protocol
func (n *SubprotocolNegotiator) HandleConnection(ctx WebSocketContext) error {
	protocol := ctx.Subprotocol()
	if protocol == "" {
		return nil // No protocol negotiated
	}

	n.mu.RLock()
	handler, ok := n.supported[protocol]
	n.mu.RUnlock()

	if !ok {
		return fmt.Errorf("unsupported protocol: %s", protocol)
	}

	// Validate the connection
	if handler.Validate != nil {
		if err := handler.Validate(ctx); err != nil {
			return fmt.Errorf("protocol validation failed: %w", err)
		}
	}

	// Initialize the protocol
	if handler.Initialize != nil {
		if err := handler.Initialize(ctx); err != nil {
			return fmt.Errorf("protocol initialization failed: %w", err)
		}
	}

	// Set close handler
	if handler.OnClose != nil {
		ctx.SetCloseHandler(func(code int, text string) error {
			return handler.OnClose(ctx)
		})
	}

	// Handle messages if handler is provided
	if handler.OnMessage != nil {
		for ctx.IsConnected() {
			msgType, data, err := ctx.ReadMessage()
			if err != nil {
				return err
			}

			if err := handler.OnMessage(ctx, msgType, data); err != nil {
				return err
			}
		}
	}

	return nil
}

// ============================================================================
// Task 5.4: Compression Support
// ============================================================================

// CompressionConfig configures WebSocket compression
type CompressionConfig struct {
	Enabled         bool
	Level           int  // Compression level (0-9)
	Threshold       int  // Minimum message size to compress (bytes)
	ContextTakeover bool // Allow context takeover
}

// DefaultCompressionConfig returns default compression configuration
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Enabled:         true,
		Level:           flate.DefaultCompression,
		Threshold:       1024, // Only compress messages > 1KB
		ContextTakeover: true,
	}
}

// CompressedMessage wraps a message with compression metadata
type CompressedMessage struct {
	Compressed bool   `json:"compressed"`
	Algorithm  string `json:"algorithm,omitempty"`
	Data       []byte `json:"data"`
}

// CompressionManager handles message compression
type CompressionManager struct {
	config CompressionConfig
	mu     sync.RWMutex
}

// NewCompressionManager creates a new compression manager
func NewCompressionManager(config CompressionConfig) *CompressionManager {
	return &CompressionManager{
		config: config,
	}
}

// ShouldCompress determines if a message should be compressed
func (m *CompressionManager) ShouldCompress(data []byte) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.config.Enabled {
		return false
	}

	return len(data) >= m.config.Threshold
}

// UpdateConfig updates the compression configuration
func (m *CompressionManager) UpdateConfig(config CompressionConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = config
}

// GetConfig returns the current compression configuration
func (m *CompressionManager) GetConfig() CompressionConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// ============================================================================
// Task 5.5: Custom Upgrader Support
// ============================================================================

// CustomUpgraderConfig allows custom upgrader configuration
type CustomUpgraderConfig struct {
	// Core upgrader settings
	ReadBufferSize   int
	WriteBufferSize  int
	HandshakeTimeout time.Duration

	// Custom settings
	EnableCompression bool
	CompressionLevel  int
	Subprotocols      []string

	// Hooks
	BeforeUpgrade func(ctx Context) error
	AfterUpgrade  func(ctx WebSocketContext) error

	// Custom validation
	ValidateHeaders func(headers map[string][]string) error
	ValidateOrigin  func(origin string) bool

	// Error handling
	ErrorHandler func(ctx Context, err error) error
}

// CustomUpgrader wraps a WebSocket upgrader with custom logic
type CustomUpgrader struct {
	config   CustomUpgraderConfig
	upgrader *websocket.Upgrader
	mu       sync.RWMutex
}

// NewCustomUpgrader creates a new custom upgrader
func NewCustomUpgrader(config CustomUpgraderConfig) *CustomUpgrader {
	upgrader := &websocket.Upgrader{
		ReadBufferSize:    config.ReadBufferSize,
		WriteBufferSize:   config.WriteBufferSize,
		HandshakeTimeout:  config.HandshakeTimeout,
		Subprotocols:      config.Subprotocols,
		EnableCompression: config.EnableCompression,
	}

	// Set origin check function
	if config.ValidateOrigin != nil {
		upgrader.CheckOrigin = func(r *http.Request) bool {
			return config.ValidateOrigin(r.Header.Get("Origin"))
		}
	}

	return &CustomUpgrader{
		config:   config,
		upgrader: upgrader,
	}
}

// Upgrade performs a custom WebSocket upgrade
func (u *CustomUpgrader) Upgrade(ctx Context) (WebSocketContext, error) {
	// Run before upgrade hook
	if u.config.BeforeUpgrade != nil {
		if err := u.config.BeforeUpgrade(ctx); err != nil {
			if u.config.ErrorHandler != nil {
				return nil, u.config.ErrorHandler(ctx, err)
			}
			return nil, err
		}
	}

	// Validate headers if custom validation is provided
	if u.config.ValidateHeaders != nil {
		headers := extractAllHeaders(ctx)
		if err := u.config.ValidateHeaders(headers); err != nil {
			if u.config.ErrorHandler != nil {
				return nil, u.config.ErrorHandler(ctx, err)
			}
			return nil, err
		}
	}

	// Perform the upgrade based on context type
	var wsCtx WebSocketContext
	var err error

	switch c := ctx.(type) {
	case *httpRouterContext:
		// HTTPRouter upgrade
		wsConfig := WebSocketConfig{
			ReadBufferSize:    u.config.ReadBufferSize,
			WriteBufferSize:   u.config.WriteBufferSize,
			HandshakeTimeout:  u.config.HandshakeTimeout,
			Subprotocols:      u.config.Subprotocols,
			EnableCompression: u.config.EnableCompression,
			CheckOrigin:       u.config.ValidateOrigin,
		}
		wsCtx, err = NewHTTPRouterWebSocketContext(c.w, c.r, c.params, wsConfig, c.views)

	case *fiberContext:
		// Fiber upgrade (mock implementation)
		wsConfig := WebSocketConfig{
			ReadBufferSize:    u.config.ReadBufferSize,
			WriteBufferSize:   u.config.WriteBufferSize,
			HandshakeTimeout:  u.config.HandshakeTimeout,
			Subprotocols:      u.config.Subprotocols,
			EnableCompression: u.config.EnableCompression,
			CheckOrigin:       u.config.ValidateOrigin,
		}
		wsCtx, err = NewFiberWebSocketContext(c.ctx, wsConfig, c.logger)

	default:
		err = fmt.Errorf("unsupported context type: %T", ctx)
	}

	if err != nil {
		if u.config.ErrorHandler != nil {
			return nil, u.config.ErrorHandler(ctx, err)
		}
		return nil, err
	}

	// Perform the actual upgrade
	if err := wsCtx.WebSocketUpgrade(); err != nil {
		if u.config.ErrorHandler != nil {
			return nil, u.config.ErrorHandler(ctx, err)
		}
		return nil, err
	}

	// Run after upgrade hook
	if u.config.AfterUpgrade != nil {
		if err := u.config.AfterUpgrade(wsCtx); err != nil {
			wsCtx.Close()
			if u.config.ErrorHandler != nil {
				return nil, u.config.ErrorHandler(ctx, err)
			}
			return nil, err
		}
	}

	return wsCtx, nil
}

// UpdateConfig updates the upgrader configuration
func (u *CustomUpgrader) UpdateConfig(config CustomUpgraderConfig) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.config = config

	// Update the underlying upgrader
	u.upgrader.ReadBufferSize = config.ReadBufferSize
	u.upgrader.WriteBufferSize = config.WriteBufferSize
	u.upgrader.HandshakeTimeout = config.HandshakeTimeout
	u.upgrader.Subprotocols = config.Subprotocols
	u.upgrader.EnableCompression = config.EnableCompression

	if config.ValidateOrigin != nil {
		u.upgrader.CheckOrigin = func(r *http.Request) bool {
			return config.ValidateOrigin(r.Header.Get("Origin"))
		}
	}
}

// extractAllHeaders extracts all headers from a context
func extractAllHeaders(ctx Context) map[string][]string {
	headers := make(map[string][]string)

	// Common headers to check
	commonHeaders := []string{
		"Origin", "Host", "User-Agent",
		"Accept", "Accept-Language", "Accept-Encoding",
		"Connection", "Upgrade",
		"Sec-WebSocket-Key", "Sec-WebSocket-Version",
		"Sec-WebSocket-Protocol", "Sec-WebSocket-Extensions",
	}

	for _, header := range commonHeaders {
		if value := ctx.Header(header); value != "" {
			headers[header] = []string{value}
		}
	}

	return headers
}

// ============================================================================
// Advanced WebSocket Middleware
// ============================================================================

// AdvancedWebSocketMiddleware creates middleware with all advanced features
func AdvancedWebSocketMiddleware(config WebSocketConfig) MiddlewareFunc {
	// Create managers
	compressionManager := NewCompressionManager(DefaultCompressionConfig())
	negotiator := NewSubprotocolNegotiator()

	// Register default protocols if configured
	for _, protocol := range config.Subprotocols {
		negotiator.Register(SubprotocolHandler{
			Name:    protocol,
			Version: "1.0",
		})
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			// Check if it's a WebSocket request
			if !isWebSocketRequest(c) {
				return next(c)
			}

			// Create custom upgrader
			upgraderConfig := CustomUpgraderConfig{
				ReadBufferSize:    config.ReadBufferSize,
				WriteBufferSize:   config.WriteBufferSize,
				HandshakeTimeout:  config.HandshakeTimeout,
				Subprotocols:      negotiator.GetSupportedProtocols(),
				EnableCompression: compressionManager.GetConfig().Enabled,
				ValidateOrigin:    config.CheckOrigin,
			}

			upgrader := NewCustomUpgrader(upgraderConfig)

			// Perform upgrade
			wsCtx, err := upgrader.Upgrade(c)
			if err != nil {
				return err
			}

			// Set up deadline manager
			deadlineManager := NewDeadlineManager(wsCtx, config)
			deadlineManager.Start()
			defer deadlineManager.Stop()

			// Handle subprotocol if negotiated
			if wsCtx.Subprotocol() != "" {
				go negotiator.HandleConnection(wsCtx)
			}

			// Continue with WebSocket context
			return next(wsCtx)
		}
	}
}

// ============================================================================
// Utility Functions
// ============================================================================

// BroadcastJSON broadcasts a JSON message to multiple connections
func BroadcastJSON(connections []WebSocketContext, v interface{}, maxSize int64) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json marshal error: %w", err)
	}

	if maxSize > 0 && int64(len(data)) > maxSize {
		return ErrWebSocketMessageTooBig(int64(len(data)), maxSize)
	}

	var errors []error
	for _, conn := range connections {
		if conn.IsConnected() {
			if err := conn.WriteMessage(TextMessage, data); err != nil {
				errors = append(errors, err)
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("broadcast failed for %d connections", len(errors))
	}

	return nil
}

// ConnectionPool manages a pool of WebSocket connections
type ConnectionPool struct {
	connections map[string]WebSocketContext
	mu          sync.RWMutex
	maxSize     int
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(maxSize int) *ConnectionPool {
	return &ConnectionPool{
		connections: make(map[string]WebSocketContext),
		maxSize:     maxSize,
	}
}

// Add adds a connection to the pool
func (p *ConnectionPool) Add(ctx WebSocketContext) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.connections) >= p.maxSize {
		return fmt.Errorf("connection pool is full")
	}

	p.connections[ctx.ConnectionID()] = ctx
	return nil
}

// Remove removes a connection from the pool
func (p *ConnectionPool) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.connections, id)
}

// Get retrieves a connection by ID
func (p *ConnectionPool) Get(id string) (WebSocketContext, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ctx, ok := p.connections[id]
	return ctx, ok
}

// GetAll returns all connections
func (p *ConnectionPool) GetAll() []WebSocketContext {
	p.mu.RLock()
	defer p.mu.RUnlock()

	conns := make([]WebSocketContext, 0, len(p.connections))
	for _, ctx := range p.connections {
		conns = append(conns, ctx)
	}
	return conns
}

// Broadcast sends a message to all connections
func (p *ConnectionPool) Broadcast(messageType int, data []byte) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, ctx := range p.connections {
		if ctx.IsConnected() {
			ctx.WriteMessage(messageType, data)
		}
	}
}

// CloseAll closes all connections
func (p *ConnectionPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ctx := range p.connections {
		ctx.Close()
	}
	p.connections = make(map[string]WebSocketContext)
}
