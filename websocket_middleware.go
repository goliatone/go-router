package router

import (
	"fmt"
	"strings"
)

// WebSocketUpgrade creates middleware that handles WebSocket upgrade requests
func WebSocketUpgrade(config WebSocketConfig) MiddlewareFunc {
	// Apply defaults to configuration
	config.ApplyDefaults()

	// Validate configuration
	if err := config.Validate(); err != nil {
		// Return middleware that always returns the validation error
		return func(next HandlerFunc) HandlerFunc {
			return func(c Context) error {
				return fmt.Errorf("websocket configuration error: %w", err)
			}
		}
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			// 1. Check if request is WebSocket upgrade
			if !isWebSocketRequest(c) {
				// Not a WebSocket request, continue as normal HTTP
				return next(c)
			}

			// 2. Validate origin if configured
			if !validateOrigin(c, config) {
				return c.Status(403).SendString("Origin not allowed")
			}

			// 3. Validate subprotocols if requested
			selectedProtocol, protocolOk := validateSubprotocols(c, config)
			if !protocolOk {
				return c.Status(400).SendString("Unsupported subprotocol")
			}

			// 4. Validate WebSocket key
			wsKey := c.Header(WebSocketKey)
			if !validateWebSocketKey(wsKey) {
				return c.Status(400).SendString("Invalid WebSocket key")
			}

			// 5. Create adapter-specific WebSocket context
			wsCtx, err := createWebSocketContext(c, config, selectedProtocol)
			if err != nil {
				return fmt.Errorf("failed to create websocket context: %w", err)
			}

			// 6. Set up WebSocket response headers
			if err := setupWebSocketHeaders(c, wsKey, selectedProtocol); err != nil {
				return fmt.Errorf("failed to setup websocket headers: %w", err)
			}

			// 7. Call event handler if configured
			if config.OnConnect != nil {
				if err := config.OnConnect(wsCtx); err != nil {
					return fmt.Errorf("websocket connect handler failed: %w", err)
				}
			}

			// 8. Continue with WebSocket context
			return next(wsCtx)
		}
	}
}

// setupWebSocketHeaders sets the required WebSocket response headers
func setupWebSocketHeaders(c Context, wsKey, protocol string) error {
	// Generate the WebSocket accept key
	acceptKey := generateWebSocketAccept(wsKey)

	// Set required headers
	c.SetHeader("Upgrade", "websocket")
	c.SetHeader("Connection", "Upgrade")
	c.SetHeader(WebSocketAccept, acceptKey)

	// Set protocol if negotiated
	if protocol != "" {
		c.SetHeader(WebSocketProtocol, protocol)
	}

	return nil
}

// createWebSocketContext creates an adapter-specific WebSocket context
func createWebSocketContext(c Context, config WebSocketConfig, protocol string) (WebSocketContext, error) {
	// Get the adapter-specific factory
	factory := getWebSocketFactory(c)
	if factory == nil {
		return nil, fmt.Errorf("no websocket factory found for context type %T", c)
	}

	// Create WebSocket context using the factory
	wsCtx, err := factory.CreateWebSocketContext(c, config)
	if err != nil {
		return nil, err
	}

	// Note: protocol parameter will be used by adapter-specific implementations
	// in Phase 3 and 4 to set the negotiated subprotocol
	_ = protocol // Suppress unused parameter warning for now

	return wsCtx, nil
}

// getWebSocketFactory returns the appropriate WebSocket factory for the context type
func getWebSocketFactory(c Context) WebSocketContextFactory {
	// Check if it's a Fiber context
	if strings.Contains(fmt.Sprintf("%T", c), "fiber") {
		return GetFiberWebSocketFactory()
	}

	// Check if it's an HTTP router context
	contextType := fmt.Sprintf("%T", c)
	if strings.Contains(contextType, "httpRouter") || strings.Contains(contextType, "HTTPRouter") {
		return GetHTTPRouterWebSocketFactory()
	}

	// Default to HTTPRouter factory if no specific match (for backwards compatibility)
	if httpRouterFactory != nil {
		return GetHTTPRouterWebSocketFactory()
	}

	// Unknown context type
	return nil
}

// WebSocketContextFactory interface for creating adapter-specific WebSocket contexts
type WebSocketContextFactory interface {
	CreateWebSocketContext(c Context, config WebSocketConfig) (WebSocketContext, error)
	SupportsWebSocket() bool
	AdapterName() string
}

// Global factory registry
var (
	fiberFactory      WebSocketContextFactory
	httpRouterFactory WebSocketContextFactory
)

// RegisterWebSocketFactory registers a WebSocket factory for an adapter
func RegisterWebSocketFactory(adapterName string, factory WebSocketContextFactory) {
	switch strings.ToLower(adapterName) {
	case "fiber":
		fiberFactory = factory
	case "httprouter":
		httpRouterFactory = factory
	}
}

// GetFiberWebSocketFactory returns the Fiber WebSocket factory
func GetFiberWebSocketFactory() WebSocketContextFactory {
	return fiberFactory
}

// GetHTTPRouterWebSocketFactory returns the HTTPRouter WebSocket factory
func GetHTTPRouterWebSocketFactory() WebSocketContextFactory {
	return httpRouterFactory
}

// DefaultWebSocketMiddleware creates WebSocket middleware with default configuration
func DefaultWebSocketMiddleware() MiddlewareFunc {
	return WebSocketUpgrade(DefaultWebSocketConfig())
}

// WebSocketMiddlewareWithOrigins creates WebSocket middleware that only allows specific origins
func WebSocketMiddlewareWithOrigins(origins ...string) MiddlewareFunc {
	config := DefaultWebSocketConfig()
	config.Origins = origins
	return WebSocketUpgrade(config)
}

// WebSocketMiddlewareWithSubprotocols creates WebSocket middleware that supports specific subprotocols
func WebSocketMiddlewareWithSubprotocols(protocols ...string) MiddlewareFunc {
	config := DefaultWebSocketConfig()
	config.Subprotocols = protocols
	return WebSocketUpgrade(config)
}
