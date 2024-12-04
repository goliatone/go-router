package router

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// ErrorResponse represents the structure of error responses
type ErrorResponse struct {
	Error struct {
		Type       string            `json:"type"`
		Message    string            `json:"message"`
		Code       int               `json:"code"`
		RequestID  string            `json:"request_id,omitempty"`
		Stack      []string          `json:"stack,omitempty"`
		Metadata   map[string]any    `json:"metadata,omitempty"`
		Validation []ValidationError `json:"validation,omitempty"`
	} `json:"error"`
}

// ValidationError represents a validation error for a specific field
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ErrorHandlerConfig allows customization of error handling behavior
type ErrorHandlerConfig struct {
	// Include stack traces in non-production environments
	IncludeStack bool
	// Custom error mapping functions
	ErrorMappers []ErrorMapper
	// Logger interface for error logging
	Logger Logger
	// Environment (development, production, etc.)
	Environment string

	GetRequestID func(c Context) string
}

// ErrorMapper is a function that can map specific error types to RouterError
type ErrorMapper func(error) *RouterError

// DefaultErrorHandlerConfig provides sensible defaults
func DefaultErrorHandlerConfig() ErrorHandlerConfig {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "production"
	}
	return ErrorHandlerConfig{
		IncludeStack: true,
		ErrorMappers: defaultErrorMappers(),
		Logger:       &defaultLogger{},
		Environment:  env,
		GetRequestID: func(c Context) string {
			return c.Header("X-Request-ID")
		},
	}
}

// WithErrorHandler wraps a handler with error handling middleware
func WithErrorHandler(handler HandlerFunc, configs ...ErrorHandlerConfig) HandlerFunc {
	// Use default config if none provided
	config := DefaultErrorHandlerConfig()
	if len(configs) > 0 {
		config = configs[0]
	}

	return func(c Context) error {
		err := handler(c)
		if err == nil {
			return nil
		}

		// Convert error to RouterError
		routerErr := mapToRouterError(err, config.ErrorMappers)

		// Get request ID from context if available
		if requestID := config.GetRequestID(c); requestID != "" {
			routerErr.RequestID = requestID
		}

		// Prepare error response
		response := prepareErrorResponse(routerErr, config)

		// Log error
		logError(config.Logger, routerErr, c)

		// Send response
		return c.JSON(routerErr.Code, response)
	}
}

// logError logs the error with context information
func logError(logger Logger, err *RouterError, c Context) {
	fields := map[string]any{
		"type":       err.Type,
		"code":       err.Code,
		"path":       c.Path(),
		"method":     c.Method(),
		"request_id": err.RequestID,
	}

	if err.Internal != nil {
		fields["internal_error"] = err.Internal.Error()
	}

	if err.Metadata != nil {
		fields["metadata"] = err.Metadata
	}

	logger.Error(err.Message, fields)
}

// mapToRouterError converts any error to RouterError
func mapToRouterError(err error, mappers []ErrorMapper) *RouterError {
	var routerErr *RouterError
	if errors.As(err, &routerErr) {
		return routerErr
	}

	for _, mapper := range mappers {
		if routerErr := mapper(err); routerErr != nil {
			return routerErr
		}
	}

	return NewInternalError(err, "An unexpected error occurred")
}

func prepareErrorResponse(err *RouterError, config ErrorHandlerConfig) ErrorResponse {
	response := ErrorResponse{}
	response.Error.Type = string(err.Type)
	response.Error.Message = err.Message
	response.Error.Code = err.Code
	response.Error.RequestID = err.RequestID
	response.Error.Metadata = err.Metadata

	if config.IncludeStack && config.Environment != "production" {
		response.Error.Stack = getStackTrace()
	}

	return response
}

func getStackTrace() []string {
	stack := make([]uintptr, 32)
	length := runtime.Callers(3, stack)
	frames := runtime.CallersFrames(stack[:length])

	var trace []string
	for {
		frame, more := frames.Next()
		// Skip standard library frames
		if !strings.Contains(frame.File, "runtime/") {
			trace = append(trace, fmt.Sprintf("%s:%d %s", frame.File, frame.Line, frame.Function))
		}
		if !more {
			break
		}
	}

	return trace
}

func defaultErrorMappers() []ErrorMapper {
	return []ErrorMapper{
		// Map validation errors
		func(err error) *RouterError {
			var validationErr interface{ ValidationErrors() []ValidationError }
			if errors.As(err, &validationErr) {
				return NewValidationError("Validation failed", map[string]any{
					"validation": validationErr.ValidationErrors(),
				})
			}
			return nil
		},
		// Map HTTP errors
		func(err error) *RouterError {
			var httpErr interface{ StatusCode() int }
			if errors.As(err, &httpErr) {
				code := httpErr.StatusCode()
				return &RouterError{
					Type:    ErrorType(http.StatusText(code)),
					Code:    code,
					Message: err.Error(),
				}
			}
			return nil
		},
	}
}
