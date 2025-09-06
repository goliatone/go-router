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

	// Timeouts
	HandshakeTimeout time.Duration
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	PingPeriod       time.Duration
	PongWait         time.Duration

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

	// Custom upgrader (adapter-specific)
	CustomUpgrader any

	// Metrics and monitoring
	EnableMetrics bool
	MetricsPrefix string
}

// DefaultWebSocketConfig returns a WebSocketConfig with sensible defaults
func DefaultWebSocketConfig() WebSocketConfig {
	return WebSocketConfig{
		Origins:                  []string{"*"},
		CheckOrigin:              nil, // nil means same-origin policy
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
	// Ensure PingPeriod is less than PongWait
	if c.PingPeriod >= c.PongWait {
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

// ApplyDefaults fills in any zero values with sensible defaults
func (c *WebSocketConfig) ApplyDefaults() {
	defaults := DefaultWebSocketConfig()

	if len(c.Origins) == 0 {
		c.Origins = defaults.Origins
	}
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
