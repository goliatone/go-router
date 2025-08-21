package router

import (
	"errors"
	"fmt"
)

// WebSocket error definitions
var (
	// Connection errors
	ErrConnectionClosed = errors.New("websocket: connection closed")
	ErrConnectionLost   = errors.New("websocket: connection lost")

	// Message errors
	ErrMessageTooLarge = errors.New("websocket: message too large")
	ErrInvalidMessage  = errors.New("websocket: invalid message format")
	ErrEmptyMessage    = errors.New("websocket: empty message")
	ErrMessageTooLong  = errors.New("websocket: message too long")

	// Rate limiting
	ErrRateLimitExceeded = errors.New("websocket: rate limit exceeded")

	// Authentication
	ErrUnauthorized    = errors.New("websocket: unauthorized")
	ErrMissingUsername = errors.New("websocket: missing username")

	// Internal errors
	ErrInternalServer = errors.New("websocket: internal server error")
)

// ErrWebSocketUpgradeFailed creates an error for WebSocket upgrade failures
func ErrWebSocketUpgradeFailed(cause error) error {
	if cause == nil {
		return errors.New("websocket: upgrade failed")
	}
	return fmt.Errorf("websocket: upgrade failed: %w", cause)
}

// ErrWebSocketMessageTooBig creates an error for oversized messages
func ErrWebSocketMessageTooBig(size int64, maxSize int64) error {
	return fmt.Errorf("websocket: message too big: %d bytes (max: %d)", size, maxSize)
}

// WSError represents a WebSocket error with a close code
type WSError struct {
	Code   int
	Reason string
}

// Error implements the error interface
func (e WSError) Error() string {
	return e.Reason
}

// Common WebSocket errors with close codes
var (
	CloseNormalError          = WSError{Code: CloseNormalClosure, Reason: "normal closure"}
	CloseGoingAwayError       = WSError{Code: CloseGoingAway, Reason: "going away"}
	CloseProtocolErrorWS      = WSError{Code: CloseProtocolError, Reason: "protocol error"}
	CloseUnsupportedDataError = WSError{Code: CloseUnsupportedData, Reason: "unsupported data"}
	CloseNoStatusError        = WSError{Code: CloseNoStatusReceived, Reason: "no status received"}
	CloseAbnormalError        = WSError{Code: CloseAbnormalClosure, Reason: "abnormal closure"}
	CloseInvalidPayloadError  = WSError{Code: CloseInvalidFramePayloadData, Reason: "invalid frame payload data"}
	ClosePolicyViolationError = WSError{Code: ClosePolicyViolation, Reason: "policy violation"}
	CloseMessageTooBigError   = WSError{Code: CloseMessageTooBig, Reason: "message too big"}
	CloseMandatoryExtError    = WSError{Code: CloseMandatoryExtension, Reason: "mandatory extension"}
	CloseInternalServerError  = WSError{Code: CloseInternalServerErr, Reason: "internal server error"}
	CloseServiceRestartError  = WSError{Code: CloseServiceRestart, Reason: "service restart"}
	CloseTryAgainLaterError   = WSError{Code: CloseTryAgainLater, Reason: "try again later"}
	CloseTLSHandshakeError    = WSError{Code: CloseTLSHandshake, Reason: "TLS handshake"}
	CloseRateLimitExceeded    = WSError{Code: ClosePolicyViolation, Reason: "rate limit exceeded"}
)
