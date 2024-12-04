package router

// WithErrorHandlerMiddleware creates a middleware that handles errors for all routes in a group
func WithErrorHandlerMiddleware(opts ...ErrorHandlerOption) HandlerFunc {
	// Use default config if none provided
	config := DefaultErrorHandlerConfig()
	for _, opt := range opts {
		opt(&config)
	}

	return func(c Context) error {
		// Store the error config in context for access by error handling
		// c.Set("error_handler_config", config)

		err := c.Next()
		if err == nil {
			return nil
		}

		// Convert error to RouterError
		routerErr := MapToRouterError(err, config.ErrorMappers)

		// Get request ID from context if available
		requestID := config.GetRequestID(c)
		if requestID != "" {
			routerErr.RequestID = requestID
		}

		response := PrepareErrorResponse(routerErr, config)

		LogError(config.Logger, routerErr, c)

		return c.JSON(routerErr.Code, response)
	}
}

// WrapHandler function to wrap handlers that return error
func WrapHandler(handler func(Context) error) HandlerFunc {
	return func(c Context) error {
		return handler(c)
	}
}
