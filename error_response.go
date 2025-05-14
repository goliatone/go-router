package router

import (
	"os"

	"github.com/goliatone/go-errors"
)

// ErrorHandlerConfig allows customization of error handling behavior
type ErrorHandlerConfig struct {
	// Include stack traces in non-production environments
	IncludeStack bool
	// Custom error mapping functions
	ErrorMappers []errors.ErrorMapper
	// Logger interface for error logging
	Logger Logger
	// Environment (development, production, etc.)
	Environment string
	// Function to extract request ID from context
	GetRequestID func(c Context) string
}

// ErrorHandlerOption defines a function that can modify ErrorHandlerConfig
type ErrorHandlerOption func(*ErrorHandlerConfig)

// WithEnvironment sets the environment for error handling
func WithEnvironment(env string) ErrorHandlerOption {
	return func(config *ErrorHandlerConfig) {
		config.Environment = env
	}
}

// WithLogger sets the logger for error handling
func WithLogger(logger Logger) ErrorHandlerOption {
	return func(config *ErrorHandlerConfig) {
		config.Logger = logger
	}
}

// WithStackTrace enables or disables stack traces
func WithStackTrace(include bool) ErrorHandlerOption {
	return func(config *ErrorHandlerConfig) {
		config.IncludeStack = include
	}
}

// WithErrorMapper adds additional error mappers
func WithErrorMapper(mapper errors.ErrorMapper) ErrorHandlerOption {
	return func(config *ErrorHandlerConfig) {
		config.ErrorMappers = append(config.ErrorMappers, mapper)
	}
}

// DefaultErrorHandlerConfig provides sensible defaults
func DefaultErrorHandlerConfig() ErrorHandlerConfig {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "production"
	}
	return ErrorHandlerConfig{
		IncludeStack: true,
		ErrorMappers: errors.DefaultErrorMappers(),
		Logger:       &defaultLogger{},
		Environment:  env,
		GetRequestID: func(c Context) string {
			return c.Header("X-Request-ID")
		},
	}
}

// LogError logs the error with context information
func LogError(logger Logger, err *errors.Error, c Context) {
	fields := map[string]any{
		"category":   err.Category.String(),
		"code":       err.Code,
		"text_code":  err.TextCode,
		"path":       c.Path(),
		"method":     c.Method(),
		"request_id": err.RequestID,
	}

	if err.Source != nil {
		fields["source_error"] = err.Source.Error()
	}

	if err.Metadata != nil {
		fields["metadata"] = err.Metadata
	}

	if len(err.ValidationErrors) > 0 {
		fields["validation_errors"] = err.ValidationErrors
	}

	logger.Error(err.Message, fields)
}

func PrepareErrorResponse(err *errors.Error, config ErrorHandlerConfig) errors.ErrorResponse {
	var stackTrace errors.StackTrace
	if config.IncludeStack && config.Environment != "production" {
		stackTrace = errors.CaptureStackTrace(1)
	}

	return err.ToErrorResponse(config.IncludeStack, stackTrace)
}
