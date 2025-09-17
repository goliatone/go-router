package router

import "github.com/goliatone/go-errors"

const ErrorHandlerConfigKey = "error_handler_config"

// WithErrorHandlerMiddleware creates a middleware that handles errors for all routes in a group
func WithErrorHandlerMiddleware(opts ...ErrorHandlerOption) MiddlewareFunc {
	config := DefaultErrorHandlerConfig()
	for _, opt := range opts {
		opt(&config)
	}

	return func(hf HandlerFunc) HandlerFunc {
		return func(c Context) error {
			// c.Set(ErrorHandlerConfigKey, config)
			err := c.Next()
			if err == nil {
				return nil
			}
			// Convert error to RouterError
			routerErr := errors.MapToError(err, config.ErrorMappers)

			if requestID := config.GetRequestID(c); requestID != "" {
				routerErr.RequestID = requestID
			}

			response := PrepareErrorResponse(routerErr, config)

			LogError(config.Logger, routerErr, c)

			return c.JSON(routerErr.Code, response)
		}
	}
}
