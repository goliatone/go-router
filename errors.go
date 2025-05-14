package router

import (
	"fmt"
	"maps"
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
	Type      ErrorType      `json:"type"`
	Code      int            `json:"code"`
	Message   string         `json:"message"`
	Internal  error          `json:"error"`
	Metadata  map[string]any `json:"meta"`
	RequestID string         `json:"request_id"`
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
	maps.Copy(e.Metadata, meta)
	return e
}

func (e *RouterError) WithRequestID(id string) *RouterError {
	e.RequestID = id
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

func NewUnauthorizedError(message string, metas ...map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeUnauthorized,
		Code:     http.StatusUnauthorized,
		Message:  message,
		Metadata: mergeMeta(metas...),
	}
}

func NewForbiddenError(message string, metas ...map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeForbidden,
		Code:     http.StatusForbidden,
		Message:  message,
		Metadata: mergeMeta(metas...),
	}
}

func NewNotFoundError(message string, metas ...map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeNotFound,
		Code:     http.StatusNotFound,
		Message:  message,
		Metadata: mergeMeta(metas...),
	}
}

func NewInternalError(err error, message string, metas ...map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeInternal,
		Code:     http.StatusInternalServerError,
		Message:  message,
		Internal: err,
		Metadata: mergeMeta(metas...),
	}
}

// NewBadRequestError for generic bad requests outside of validation context
func NewBadRequestError(message string, metas ...map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeBadRequest,
		Code:     http.StatusBadRequest,
		Message:  message,
		Metadata: mergeMeta(metas...),
	}
}

// NewConflictError for requests that could not be completed due to a conflict
func NewConflictError(message string, metas ...map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeConflict,
		Code:     http.StatusConflict,
		Message:  message,
		Metadata: mergeMeta(metas...),
	}
}

// NewTooManyRequestsError for rate-limiting scenarios
func NewTooManyRequestsError(message string, metas ...map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeTooManyRequests,
		Code:     http.StatusTooManyRequests,
		Message:  message,
		Metadata: mergeMeta(metas...),
	}
}

// NewMethodNotAllowedError for requests that use an unallowed HTTP method
func NewMethodNotAllowedError(message string, metas ...map[string]any) *RouterError {
	return &RouterError{
		Type:     ErrorTypeMethodNotAllowed,
		Code:     http.StatusMethodNotAllowed,
		Message:  message,
		Metadata: mergeMeta(metas...),
	}
}

func mergeMeta(metas ...map[string]any) map[string]any {
	out := map[string]any{}
	if len(metas) == 0 {
		return out
	}

	for _, meta := range metas {
		maps.Copy(out, meta)
	}

	return out
}
