package router

import (
	"time"
)

// WebSocket message types (compatible with RFC 6455)
const (
	TextMessage   = 1
	BinaryMessage = 2
	CloseMessage  = 8
	PingMessage   = 9
	PongMessage   = 10
)

// WebSocket close codes (RFC 6455 compliant)
const (
	CloseNormalClosure           = 1000
	CloseGoingAway               = 1001
	CloseProtocolError           = 1002
	CloseUnsupportedData         = 1003
	CloseNoStatusReceived        = 1005
	CloseAbnormalClosure         = 1006
	CloseInvalidFramePayloadData = 1007
	ClosePolicyViolation         = 1008
	CloseMessageTooBig           = 1009
	CloseMandatoryExtension      = 1010
	CloseInternalServerErr       = 1011
	CloseServiceRestart          = 1012
	CloseTryAgainLater           = 1013
	CloseTLSHandshake            = 1015
)

// WebSocket headers
const (
	WebSocketKey        = "Sec-WebSocket-Key"
	WebSocketVersion    = "Sec-WebSocket-Version"
	WebSocketProtocol   = "Sec-WebSocket-Protocol"
	WebSocketExtensions = "Sec-WebSocket-Extensions"
	WebSocketAccept     = "Sec-WebSocket-Accept"
)

// UpgradeData stores extracted data from HTTP context before WebSocket upgrade
type UpgradeData map[string]any

// WebSocketConfig contains configuration options for WebSocket connections
type WebSocketConfig struct {
	// Origin validation
	Origins     []string
	CheckOrigin func(origin string) bool

	// Protocol negotiation
	Subprotocols []string

	// Buffer sizes
	ReadBufferSize  int
	WriteBufferSize int

	// Timeouts and Keep-Alive Configuration
	//
	// HandshakeTimeout: Maximum time allowed for the WebSocket handshake to complete.
	// If the client doesn't complete the handshake within this time, the connection
	// is rejected. Default: 10 seconds.
	HandshakeTimeout time.Duration

	// ReadTimeout: Retained for compatibility with higher-level WebSocket helpers.
	// Adapter read deadlines use PongWait so idle but healthy connections can stay
	// open when automatic ping/pong keepalive is enabled. Set DisableReadDeadline
	// to true to disable adapter-managed read deadlines. Default: 60 seconds.
	ReadTimeout time.Duration

	// WriteTimeout: Maximum time allowed for write operations to the WebSocket.
	// If a write operation (like sending a message) takes longer than this,
	// it will timeout and the connection may be closed. Default: 10 seconds.
	WriteTimeout time.Duration

	// PingPeriod: How often to send ping frames to the client to check if the
	// connection is still alive. Must be less than PongWait. The server sends
	// ping frames at this interval to detect broken connections. Default: 54 seconds.
	// Set DisableKeepAlive to true to disable automatic pings; zero uses defaults.
	PingPeriod time.Duration

	// PongWait: Maximum time to wait for a pong response after sending a ping.
	// If the client doesn't respond with a pong within this time after receiving
	// a ping, the connection is considered dead and will be closed. This is the
	// primary mechanism for detecting broken connections. Default: 60 seconds.
	// Must be greater than PingPeriod when keepalive and read deadlines are enabled.
	PongWait time.Duration

	// DisableKeepAlive disables go-router's automatic server-side ping loop and
	// pong-based read deadline management. Zero duration fields still mean
	// "apply defaults"; use this flag when an endpoint must opt out.
	DisableKeepAlive bool

	// DisableReadDeadline disables adapter-managed WebSocket read deadlines while
	// allowing automatic server pings to continue. This is useful for endpoints
	// that want keepalive traffic but manage stale connection cleanup elsewhere.
	DisableReadDeadline bool

	// Message limits
	MaxMessageSize int64

	// Compression
	EnableCompression bool
	CompressionLevel  int

	// Connection management
	AllowMultipleConnections bool
	ConnectionPoolSize       int

	// Event handlers
	OnConnect    func(WebSocketContext) error
	OnDisconnect func(WebSocketContext, error)
	OnMessage    func(WebSocketContext, int, []byte) error
	OnError      func(WebSocketContext, error)
	OnPreUpgrade func(Context) (UpgradeData, error)

	// Custom upgrader (adapter-specific)
	CustomUpgrader any

	// Metrics and monitoring
	EnableMetrics bool
	MetricsPrefix string
}

// DefaultWebSocketConfig returns a WebSocketConfig with sensible defaults that
// prevent common WebSocket issues like hanging connections and resource leaks.
//
// Key timeout defaults prevent connection issues:
//   - WriteTimeout (10s): Ensures write operations don't hang the server
//   - PingPeriod (54s) + PongWait (60s): Automatic ping/pong health checking
//   - HandshakeTimeout (10s): Prevents slow handshake attacks
//
// These defaults ensure that:
//  1. Dead connections are detected and cleaned up automatically
//  2. Server resources (goroutines, memory, locks) are released properly
//  3. APIs and endpoints don't hang due to zombie WebSocket connections
//  4. The server remains responsive under load
func DefaultWebSocketConfig() WebSocketConfig {
	return WebSocketConfig{
		Origins:                  []string{},
		CheckOrigin:              nil, // nil + empty Origins means same-origin policy
		Subprotocols:             []string{},
		ReadBufferSize:           4096,
		WriteBufferSize:          4096,
		HandshakeTimeout:         10 * time.Second,
		ReadTimeout:              60 * time.Second,
		WriteTimeout:             10 * time.Second,
		PingPeriod:               54 * time.Second, // Must be less than PongWait
		PongWait:                 60 * time.Second,
		MaxMessageSize:           1024 * 1024, // 1MB
		EnableCompression:        false,
		CompressionLevel:         -1, // Default compression
		AllowMultipleConnections: true,
		ConnectionPoolSize:       100,
		OnConnect:                nil,
		OnDisconnect:             nil,
		OnMessage:                nil,
		OnError:                  nil,
		CustomUpgrader:           nil,
		EnableMetrics:            false,
		MetricsPrefix:            "websocket_",
	}
}

// Validate checks the WebSocketConfig for common configuration errors
func (c *WebSocketConfig) Validate() error {
	// Ensure PingPeriod is less than PongWait when pong deadlines are active.
	if c.readDeadlineEnabled() && c.PingPeriod >= c.PongWait {
		return NewValidationError("PingPeriod must be less than PongWait", nil)
	}

	// Ensure reasonable buffer sizes
	if c.ReadBufferSize < 1024 {
		return NewValidationError("ReadBufferSize should be at least 1024 bytes", nil)
	}
	if c.WriteBufferSize < 1024 {
		return NewValidationError("WriteBufferSize should be at least 1024 bytes", nil)
	}

	// Ensure reasonable timeouts
	if c.HandshakeTimeout < time.Second {
		return NewValidationError("HandshakeTimeout should be at least 1 second", nil)
	}

	// Validate compression level
	if c.EnableCompression && (c.CompressionLevel < -1 || c.CompressionLevel > 9) {
		return NewValidationError("CompressionLevel must be between -1 and 9", nil)
	}

	// Validate message size limit
	if c.MaxMessageSize < 1024 {
		return NewValidationError("MaxMessageSize should be at least 1024 bytes", nil)
	}

	return nil
}

func (c WebSocketConfig) keepAliveEnabled() bool {
	return !c.DisableKeepAlive && c.PingPeriod > 0
}

func (c WebSocketConfig) readDeadlineEnabled() bool {
	return !c.DisableKeepAlive && !c.DisableReadDeadline && c.PongWait > 0
}

// GetUpgradeDataWithDefault is a convenience function for WebSocket contexts
func GetUpgradeDataWithDefault(ws WebSocketContext, key string, defaultValue any) any {
	if value, exists := ws.UpgradeData(key); exists {
		return value
	}
	return defaultValue
}

// ApplyDefaults fills in any zero values with sensible defaults
func (c *WebSocketConfig) ApplyDefaults() {
	defaults := DefaultWebSocketConfig()

	if len(c.Subprotocols) == 0 {
		c.Subprotocols = defaults.Subprotocols
	}
	if c.ReadBufferSize == 0 {
		c.ReadBufferSize = defaults.ReadBufferSize
	}
	if c.WriteBufferSize == 0 {
		c.WriteBufferSize = defaults.WriteBufferSize
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = defaults.HandshakeTimeout
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = defaults.ReadTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = defaults.WriteTimeout
	}
	if c.PingPeriod == 0 {
		c.PingPeriod = defaults.PingPeriod
	}
	if c.PongWait == 0 {
		c.PongWait = defaults.PongWait
	}
	if c.MaxMessageSize == 0 {
		c.MaxMessageSize = defaults.MaxMessageSize
	}
	if c.CompressionLevel == 0 {
		c.CompressionLevel = defaults.CompressionLevel
	}
	if c.ConnectionPoolSize == 0 {
		c.ConnectionPoolSize = defaults.ConnectionPoolSize
	}
	if c.MetricsPrefix == "" {
		c.MetricsPrefix = defaults.MetricsPrefix
	}
}
