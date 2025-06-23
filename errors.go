package router

import (
	"net/http"

	"github.com/goliatone/go-errors"
)

// NewValidationError
func NewValidationError(message string, validationErrs []errors.FieldError, metas ...map[string]any) *errors.Error {
	return errors.NewValidation(message, validationErrs...).
		WithCode(http.StatusBadRequest).
		WithTextCode("VALIDATION_ERROR").
		WithMetadata(metas...)
}

func NewUnauthorizedError(message string, metas ...map[string]any) *errors.Error {
	return errors.New(message, errors.CategoryAuth).
		WithCode(http.StatusUnauthorized).
		WithTextCode("UNAUTHORIZED").
		WithMetadata(metas...)
}

func NewForbiddenError(message string, metas ...map[string]any) *errors.Error {
	return errors.New(message, errors.CategoryAuthz).
		WithCode(http.StatusForbidden).
		WithTextCode("FORBIDDEN").
		WithMetadata(metas...)
}

func NewNotFoundError(message string, metas ...map[string]any) *errors.Error {
	return errors.New(message, errors.CategoryNotFound).
		WithCode(http.StatusNotFound).
		WithTextCode("NOT_FOUND").
		WithMetadata(metas...)
}

func NewInternalError(err error, message string, metas ...map[string]any) *errors.Error {
	return errors.Wrap(err, errors.CategoryInternal, message).
		WithCode(http.StatusInternalServerError).
		WithTextCode("INTERNAL_ERROR").
		WithMetadata(metas...)
}

// NewBadRequestError for generic bad requests outside of validation context
func NewBadRequestError(message string, metas ...map[string]any) *errors.Error {
	return errors.New(message, errors.CategoryBadInput).
		WithCode(http.StatusBadRequest).
		WithTextCode("BAD_REQUEST").
		WithMetadata(metas...)
}

// NewConflictError for requests that could not be completed due to a conflict
func NewConflictError(message string, metas ...map[string]any) *errors.Error {
	return errors.New(message, errors.CategoryConflict).
		WithCode(http.StatusConflict).
		WithTextCode("CONFLICT").
		WithMetadata(metas...)
}

// NewTooManyRequestsError for rate-limiting scenarios
func NewTooManyRequestsError(message string, metas ...map[string]any) *errors.Error {
	return errors.New(message, errors.CategoryRateLimit).
		WithCode(http.StatusTooManyRequests).
		WithTextCode("TOO_MANY_REQUESTS").
		WithMetadata(metas...)
}

// NewMethodNotAllowedError for requests that use an unallowed HTTP method
func NewMethodNotAllowedError(message string, metas ...map[string]any) *errors.Error {
	return errors.New(message, errors.CategoryMethodNotAllowed).
		WithCode(http.StatusMethodNotAllowed).
		WithTextCode("METHOD_NOT_ALLOWED").
		WithMetadata(metas...)
}

func NewMiddlewareError(err error, message string, metas ...map[string]any) *errors.Error {
	return errors.Wrap(err, errors.CategoryMiddleware, message).
		WithCode(http.StatusInternalServerError).
		WithTextCode("MIDDLEWARE_ERROR").
		WithMetadata(metas...)
}

func NewRoutingError(message string, metas ...map[string]any) *errors.Error {
	return errors.New(message, errors.CategoryRouting).
		WithCode(http.StatusNotFound).
		WithTextCode("ROUTING_ERROR").
		WithMetadata(metas...)
}

func NewHandlerError(err error, message string, metas ...map[string]any) *errors.Error {
	return errors.Wrap(err, errors.CategoryHandler, message).
		WithCode(http.StatusInternalServerError).
		WithTextCode("HANDLER_ERROR").
		WithMetadata(metas...)
}
