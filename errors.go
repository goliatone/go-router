package router

import (
	"fmt"
	"net/http"
)

// Error Types
type ErrorType string

const (
	ErrorTypeValidation   ErrorType = "VALIDATION_ERROR"
	ErrorTypeMiddleware   ErrorType = "MIDDLEWARE_ERROR"
	ErrorTypeRouting      ErrorType = "ROUTING_ERROR"
	ErrorTypeHandler      ErrorType = "HANDLER_ERROR"
	ErrorTypeInternal     ErrorType = "INTERNAL_ERROR"
	ErrorTypeUnauthorized ErrorType = "UNAUTHORIZED"
	ErrorTypeForbidden    ErrorType = "FORBIDDEN"
	ErrorTypeNotFound     ErrorType = "NOT_FOUND"
)

// RouterError represents a custom error type for the router package
type RouterError struct {
	Type      ErrorType
	Code      int
	Message   string
	Internal  error
	Metadata  map[string]any
	RequestID string
}

func (e *RouterError) Error() string {
	if e.Internal != nil {
		return fmt.Sprintf("%s: %s (internal: %v)", e.Type, e.Message, e.Internal)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *RouterError) WithMetadata(meta map[string]any) *RouterError {
	for k, v := range meta {
		e.Metadata[k] = v
	}
	return e
}

func (e *RouterError) Unwrap() error {
	return e.Internal
}

// Error constructors
func NewValidationError(message string, metadata map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeValidation,
		Code:     http.StatusBadRequest,
		Message:  message,
		Metadata: metadata,
	}
}

func NewUnauthorizedError(message string) *RouterError {
	return &RouterError{
		Type:     ErrorTypeUnauthorized,
		Code:     http.StatusUnauthorized,
		Message:  message,
		Metadata: map[string]any{},
	}
}

func NewForbiddenError(message string) *RouterError {
	return &RouterError{
		Type:     ErrorTypeForbidden,
		Code:     http.StatusForbidden,
		Message:  message,
		Metadata: map[string]any{},
	}
}

func NewNotFoundError(message string) *RouterError {
	return &RouterError{
		Type:     ErrorTypeNotFound,
		Code:     http.StatusNotFound,
		Message:  message,
		Metadata: map[string]any{},
	}
}

func NewInternalError(err error, message string) *RouterError {
	return &RouterError{
		Type:     ErrorTypeInternal,
		Code:     http.StatusInternalServerError,
		Message:  message,
		Internal: err,
		Metadata: map[string]any{},
	}
}
