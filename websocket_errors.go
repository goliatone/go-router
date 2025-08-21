package router

import (
	"fmt"
	"net/http"
	"time"
)

// WebSocketErrorType represents different categories of WebSocket errors
type WebSocketErrorType string

const (
	// Connection errors
	WebSocketErrorConnection WebSocketErrorType = "connection"
	WebSocketErrorUpgrade    WebSocketErrorType = "upgrade"
	WebSocketErrorHandshake  WebSocketErrorType = "handshake"
	WebSocketErrorAuth       WebSocketErrorType = "auth"
	WebSocketErrorTimeout    WebSocketErrorType = "timeout"

	// Protocol errors
	WebSocketErrorProtocol WebSocketErrorType = "protocol"
	WebSocketErrorFrame    WebSocketErrorType = "frame"
	WebSocketErrorMessage  WebSocketErrorType = "message"
	WebSocketErrorClose    WebSocketErrorType = "close"

	// Configuration errors
	WebSocketErrorConfig     WebSocketErrorType = "config"
	WebSocketErrorValidation WebSocketErrorType = "validation"
	WebSocketErrorSecurity   WebSocketErrorType = "security"

	// Runtime errors
	WebSocketErrorRuntime WebSocketErrorType = "runtime"
	WebSocketErrorIO      WebSocketErrorType = "io"
	WebSocketErrorNetwork WebSocketErrorType = "network"

	// Application errors
	WebSocketErrorApplication WebSocketErrorType = "application"
	WebSocketErrorHandlerType WebSocketErrorType = "handler"
)

// WebSocketError represents a comprehensive WebSocket error
type WebSocketError struct {
	Type        WebSocketErrorType `json:"type"`
	Code        int                `json:"code"`
	Message     string             `json:"message"`
	Details     string             `json:"details,omitempty"`
	Timestamp   time.Time          `json:"timestamp"`
	Recoverable bool               `json:"recoverable"`
	Cause       error              `json:"-"` // Original error, not serialized
	Context     map[string]any     `json:"context,omitempty"`
}

// Error implements the error interface
func (e *WebSocketError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("websocket %s error [%d]: %s (%s)", e.Type, e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("websocket %s error [%d]: %s", e.Type, e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *WebSocketError) Unwrap() error {
	return e.Cause
}

// IsRecoverable returns whether this error allows the connection to continue
func (e *WebSocketError) IsRecoverable() bool {
	return e.Recoverable
}

// HTTPStatus returns the appropriate HTTP status code for this error
func (e *WebSocketError) HTTPStatus() int {
	switch e.Type {
	case WebSocketErrorAuth, WebSocketErrorSecurity:
		return http.StatusForbidden
	case WebSocketErrorUpgrade, WebSocketErrorHandshake, WebSocketErrorProtocol:
		return http.StatusBadRequest
	case WebSocketErrorTimeout:
		return http.StatusRequestTimeout
	case WebSocketErrorConfig, WebSocketErrorValidation:
		return http.StatusBadRequest
	case WebSocketErrorConnection, WebSocketErrorNetwork:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// NewWebSocketError creates a new WebSocket error
func NewWebSocketError(errorType WebSocketErrorType, code int, message string) *WebSocketError {
	return &WebSocketError{
		Type:        errorType,
		Code:        code,
		Message:     message,
		Timestamp:   time.Now(),
		Recoverable: false,
		Context:     make(map[string]any),
	}
}

// NewWebSocketErrorWithCause creates a new WebSocket error with an underlying cause
func NewWebSocketErrorWithCause(errorType WebSocketErrorType, code int, message string, cause error) *WebSocketError {
	return &WebSocketError{
		Type:        errorType,
		Code:        code,
		Message:     message,
		Timestamp:   time.Now(),
		Recoverable: false,
		Cause:       cause,
		Context:     make(map[string]any),
	}
}

// WithDetails adds details to the error
func (e *WebSocketError) WithDetails(details string) *WebSocketError {
	e.Details = details
	return e
}

// WithContext adds context information to the error
func (e *WebSocketError) WithContext(key string, value any) *WebSocketError {
	if e.Context == nil {
		e.Context = make(map[string]any)
	}
	e.Context[key] = value
	return e
}

// AsRecoverable marks the error as recoverable
func (e *WebSocketError) AsRecoverable() *WebSocketError {
	e.Recoverable = true
	return e
}

// Predefined WebSocket errors for common scenarios

// Connection errors
func ErrWebSocketUpgradeFailed(cause error) *WebSocketError {
	return NewWebSocketErrorWithCause(WebSocketErrorUpgrade, 1001, "WebSocket upgrade failed", cause)
}

func ErrWebSocketHandshakeFailed(details string) *WebSocketError {
	return NewWebSocketError(WebSocketErrorHandshake, 1002, "WebSocket handshake failed").WithDetails(details)
}

func ErrWebSocketConnectionClosed() *WebSocketError {
	return NewWebSocketError(WebSocketErrorConnection, 1003, "WebSocket connection closed")
}

func ErrWebSocketConnectionTimeout() *WebSocketError {
	return NewWebSocketError(WebSocketErrorTimeout, 1004, "WebSocket connection timeout")
}

// Protocol errors
func ErrWebSocketInvalidFrame(details string) *WebSocketError {
	return NewWebSocketError(WebSocketErrorFrame, 1005, "Invalid WebSocket frame").WithDetails(details)
}

func ErrWebSocketMessageTooBig(size int64, maxSize int64) *WebSocketError {
	return NewWebSocketError(WebSocketErrorMessage, 1006, "WebSocket message too big").
		WithDetails(fmt.Sprintf("message size: %d, max allowed: %d", size, maxSize))
}

func ErrWebSocketUnsupportedProtocol(protocol string) *WebSocketError {
	return NewWebSocketError(WebSocketErrorProtocol, 1007, "Unsupported WebSocket protocol").
		WithDetails("protocol: " + protocol)
}

// Security errors
func ErrWebSocketOriginNotAllowed(origin string) *WebSocketError {
	return NewWebSocketError(WebSocketErrorSecurity, 1008, "Origin not allowed").
		WithDetails("origin: " + origin)
}

func ErrWebSocketAuthenticationFailed() *WebSocketError {
	return NewWebSocketError(WebSocketErrorAuth, 1009, "WebSocket authentication failed")
}

// Configuration errors
func ErrWebSocketInvalidConfig(details string) *WebSocketError {
	return NewWebSocketError(WebSocketErrorConfig, 1010, "Invalid WebSocket configuration").
		WithDetails(details)
}

func ErrWebSocketAdapterNotSupported(adapterType string) *WebSocketError {
	return NewWebSocketError(WebSocketErrorConfig, 1011, "WebSocket adapter not supported").
		WithDetails("adapter: " + adapterType)
}

// WSErrorHandler defines how to handle WebSocket errors
type WSErrorHandler struct {
	// Error logging function
	Logger func(err *WebSocketError)

	// Error recovery function - returns true if connection should continue
	Recovery func(err *WebSocketError, ctx WebSocketContext) bool

	// Error response function - customizes error responses
	Response func(err *WebSocketError, ctx Context) error

	// Metrics function - records error metrics
	Metrics func(err *WebSocketError)

	// Notification function - sends error notifications
	Notify func(err *WebSocketError)
}

// DefaultWebSocketErrorHandler returns a default error handler
func DefaultWebSocketErrorHandler() *WSErrorHandler {
	return &WSErrorHandler{
		Logger: func(err *WebSocketError) {
			// Default logging - in production, use structured logging
			fmt.Printf("WebSocket Error [%s:%d]: %s\n", err.Type, err.Code, err.Message)
		},
		Recovery: func(err *WebSocketError, ctx WebSocketContext) bool {
			// Only recover from recoverable errors
			return err.IsRecoverable()
		},
		Response: func(err *WebSocketError, ctx Context) error {
			// Send appropriate HTTP error response
			status := err.HTTPStatus()
			message := err.Message
			if err.Details != "" {
				message += ": " + err.Details
			}
			return ctx.Status(status).SendString(message)
		},
		Metrics: func(err *WebSocketError) {
			// Default metrics - no-op
		},
		Notify: func(err *WebSocketError) {
			// Default notifications - no-op
		},
	}
}

// HandleWebSocketError processes a WebSocket error using the configured handler
func HandleWebSocketError(err error, ctx Context, wsCtx WebSocketContext, handler *WSErrorHandler) error {
	if handler == nil {
		handler = DefaultWebSocketErrorHandler()
	}

	// Convert to WebSocketError if needed
	var wsErr *WebSocketError
	if we, ok := err.(*WebSocketError); ok {
		wsErr = we
	} else {
		// Wrap unknown error
		wsErr = NewWebSocketErrorWithCause(WebSocketErrorRuntime, 2000, "Unexpected error", err)
	}

	// Add context information
	wsErr.WithContext("timestamp", time.Now())
	if ctx != nil {
		wsErr.WithContext("method", ctx.Method())
		wsErr.WithContext("path", ctx.Path())
		wsErr.WithContext("user_agent", ctx.Header("User-Agent"))
	}

	// Log the error
	if handler.Logger != nil {
		handler.Logger(wsErr)
	}

	// Record metrics
	if handler.Metrics != nil {
		handler.Metrics(wsErr)
	}

	// Send notifications for severe errors
	if handler.Notify != nil && !wsErr.IsRecoverable() {
		handler.Notify(wsErr)
	}

	// Try to recover if this is a WebSocket context
	if wsCtx != nil && handler.Recovery != nil {
		if handler.Recovery(wsErr, wsCtx) {
			return nil // Error was recovered
		}
	}

	// Return error response
	if handler.Response != nil && ctx != nil {
		return handler.Response(wsErr, ctx)
	}

	return wsErr
}

// ErrorRecoveryMiddleware creates middleware that handles WebSocket errors gracefully
func ErrorRecoveryMiddleware(handler *WSErrorHandler) MiddlewareFunc {
	if handler == nil {
		handler = DefaultWebSocketErrorHandler()
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			defer func() {
				if r := recover(); r != nil {
					// Convert panic to WebSocket error
					var err error
					if e, ok := r.(error); ok {
						err = e
					} else {
						err = fmt.Errorf("panic: %v", r)
					}

					panicErr := NewWebSocketErrorWithCause(WebSocketErrorRuntime, 3000, "Handler panic", err)
					panicErr.WithDetails(fmt.Sprintf("panic: %v", r))

					// Handle the panic as a WebSocket error
					HandleWebSocketError(panicErr, c, nil, handler)
				}
			}()

			// Execute the handler
			err := next(c)
			if err != nil {
				// Check if this is already a WebSocketError
				if wsErr, ok := err.(*WebSocketError); ok {
					return HandleWebSocketError(wsErr, c, nil, handler)
				}

				// Try to detect if this is a WebSocket context
				var wsCtx WebSocketContext
				if wc, ok := c.(WebSocketContext); ok {
					wsCtx = wc
				}

				return HandleWebSocketError(err, c, wsCtx, handler)
			}

			return nil
		}
	}
}

// ConnectionCleanupError handles connection cleanup errors
func ConnectionCleanupError(connID string, err error) *WebSocketError {
	return NewWebSocketErrorWithCause(WebSocketErrorConnection, 4000, "Connection cleanup failed", err).
		WithContext("connection_id", connID).
		AsRecoverable() // Cleanup errors shouldn't stop the application
}

// MessageProcessingError handles message processing errors
func MessageProcessingError(messageType int, err error) *WebSocketError {
	return NewWebSocketErrorWithCause(WebSocketErrorMessage, 4001, "Message processing failed", err).
		WithContext("message_type", messageType).
		AsRecoverable() // Continue processing other messages
}

// UpgradeValidationError handles upgrade validation errors
func UpgradeValidationError(field, value string) *WebSocketError {
	return NewWebSocketError(WebSocketErrorValidation, 4002, "Upgrade validation failed").
		WithDetails(fmt.Sprintf("field: %s, value: %s", field, value))
}

// IsWebSocketError checks if an error is a WebSocket error
func IsWebSocketError(err error) bool {
	_, ok := err.(*WebSocketError)
	return ok
}

// ExtractWebSocketError extracts a WebSocketError from any error
func ExtractWebSocketError(err error) *WebSocketError {
	if wsErr, ok := err.(*WebSocketError); ok {
		return wsErr
	}

	// Check if it's wrapped
	if wsErr := findWebSocketError(err); wsErr != nil {
		return wsErr
	}

	// Create a generic WebSocket error
	return NewWebSocketErrorWithCause(WebSocketErrorRuntime, 5000, "Unknown error", err)
}

// findWebSocketError recursively searches for a WebSocketError in wrapped errors
func findWebSocketError(err error) *WebSocketError {
	if err == nil {
		return nil
	}

	if wsErr, ok := err.(*WebSocketError); ok {
		return wsErr
	}

	// Try to unwrap
	if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
		return findWebSocketError(unwrapper.Unwrap())
	}

	return nil
}

// WebSocketErrorCode constants for common error codes
const (
	CodeUpgradeFailed        = 1001
	CodeHandshakeFailed      = 1002
	CodeConnectionClosed     = 1003
	CodeConnectionTimeout    = 1004
	CodeInvalidFrame         = 1005
	CodeMessageTooBig        = 1006
	CodeUnsupportedProtocol  = 1007
	CodeOriginNotAllowed     = 1008
	CodeAuthenticationFailed = 1009
	CodeInvalidConfig        = 1010
	CodeAdapterNotSupported  = 1011
	CodeHandlerPanic         = 3000
	CodeCleanupFailed        = 4000
	CodeMessageProcessing    = 4001
	CodeUpgradeValidation    = 4002
	CodeUnknownError         = 5000
)
