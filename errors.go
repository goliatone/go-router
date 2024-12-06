package router

import (
	"fmt"
	"net/http"
)

// Error Types
type ErrorType string

const (
	ErrorTypeValidation       ErrorType = "VALIDATION_ERROR"
	ErrorTypeMiddleware       ErrorType = "MIDDLEWARE_ERROR"
	ErrorTypeRouting          ErrorType = "ROUTING_ERROR"
	ErrorTypeHandler          ErrorType = "HANDLER_ERROR"
	ErrorTypeInternal         ErrorType = "INTERNAL_ERROR"
	ErrorTypeUnauthorized     ErrorType = "UNAUTHORIZED"
	ErrorTypeForbidden        ErrorType = "FORBIDDEN"
	ErrorTypeNotFound         ErrorType = "NOT_FOUND"
	ErrorTypeBadRequest       ErrorType = "BAD_REQUEST"
	ErrorTypeConflict         ErrorType = "CONFLICT"
	ErrorTypeTooManyRequests  ErrorType = "TOO_MANY_REQUESTS"
	ErrorTypeMethodNotAllowed ErrorType = "METHOD_NOT_ALLOWED"
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
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}

	for k, v := range meta {
		e.Metadata[k] = v
	}
	return e
}

func (e *RouterError) Unwrap() error {
	return e.Internal
}

// NewValidationError
func NewValidationError(message string, validationErrs []ValidationError) *RouterError {
	return &RouterError{
		Type:    ErrorTypeValidation,
		Code:    http.StatusBadRequest,
		Message: message,
		Metadata: map[string]any{
			"validation": validationErrs,
		},
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

// NewBadRequestError for generic bad requests outside of validation context
func NewBadRequestError(message string) *RouterError {
	return &RouterError{
		Type:     ErrorTypeBadRequest,
		Code:     http.StatusBadRequest,
		Message:  message,
		Metadata: map[string]any{},
	}
}

// NewConflictError for requests that could not be completed due to a conflict
func NewConflictError(message string) *RouterError {
	return &RouterError{
		Type:     ErrorTypeConflict,
		Code:     http.StatusConflict,
		Message:  message,
		Metadata: map[string]any{},
	}
}

// NewTooManyRequestsError for rate-limiting scenarios
func NewTooManyRequestsError(message string) *RouterError {
	return &RouterError{
		Type:     ErrorTypeTooManyRequests,
		Code:     http.StatusTooManyRequests,
		Message:  message,
		Metadata: map[string]any{},
	}
}

// NewMethodNotAllowedError for requests that use an unallowed HTTP method
func NewMethodNotAllowedError(message string) *RouterError {
	return &RouterError{
		Type:     ErrorTypeMethodNotAllowed,
		Code:     http.StatusMethodNotAllowed,
		Message:  message,
		Metadata: map[string]any{},
	}
}
