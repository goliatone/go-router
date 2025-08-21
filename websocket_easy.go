package router

import (
	"context"
)

// EasyWebSocket creates a simple WebSocket handler with minimal configuration
func EasyWebSocket(handler func(context.Context, WSClient) error) func(Context) error {
	hub := NewWSHub()

	// Set up the connection handler
	hub.OnConnect(func(ctx context.Context, client WSClient, _ interface{}) error {
		return handler(ctx, client)
	})

	return hub.Handler()
}

// SimpleWSHandler is a convenience type for simple WebSocket handlers
type SimpleWSHandler func(ctx context.Context, client WSClient) error

// WebSocketMiddleware represents middleware for WebSocket connections
type WebSocketMiddleware func(next SimpleWSHandler) SimpleWSHandler

// WSAuth returns a WebSocket authentication middleware
func WSAuth() WebSocketMiddleware {
	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			// Add authentication logic here
			// For now, just pass through
			return next(ctx, client)
		}
	}
}

// WSLogger returns a WebSocket logging middleware
func WSLogger() WebSocketMiddleware {
	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			// Add logging logic here
			// For now, just pass through
			return next(ctx, client)
		}
	}
}

// WSMetrics returns a WebSocket metrics middleware
func WSMetrics() WebSocketMiddleware {
	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			// Add metrics logic here
			// For now, just pass through
			return next(ctx, client)
		}
	}
}

// WSRecover returns a WebSocket panic recovery middleware
func WSRecover() WebSocketMiddleware {
	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			defer func() {
				if r := recover(); r != nil {
					// Log the panic
					client.Close(CloseInternalServerErr, "internal error")
				}
			}()
			return next(ctx, client)
		}
	}
}

// WSRateLimit returns a WebSocket rate limiting middleware
func WSRateLimit(requestsPerMinute int) WebSocketMiddleware {
	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			// Add rate limiting logic here
			// For now, just pass through
			return next(ctx, client)
		}
	}
}

// ChainWSMiddleware chains multiple WebSocket middlewares
func ChainWSMiddleware(middlewares ...WebSocketMiddleware) WebSocketMiddleware {
	return func(next SimpleWSHandler) SimpleWSHandler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}
