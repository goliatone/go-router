package router

import (
	"fmt"
	"maps"
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

			// 4. Execute OnPreUpgrade hook if configured
			var upgradeData UpgradeData
			if config.OnPreUpgrade != nil {
				data, err := config.OnPreUpgrade(c)
				if err != nil {
					return writeWebSocketPreUpgradeError(c, err)
				}
				upgradeData = data
			}

			// 5. Validate WebSocket key
			wsKey := c.Header(WebSocketKey)
			if !validateWebSocketKey(wsKey) {
				return c.Status(400).SendString("Invalid WebSocket key")
			}

			// Fiber upgrades must execute the rest of the handler chain inside the
			// upgrader callback because fasthttp hijacks the request lifecycle.
			if _, ok := c.(*fiberV3Context); ok {
				return handleFiberV3MiddlewareWebSocketUpgrade(c, config, upgradeData)
			}
			if _, ok := c.(*fiberContext); ok {
				return handleFiberMiddlewareWebSocketUpgrade(c, config, upgradeData)
			}

			// 6. Create adapter-specific WebSocket context
			wsCtx, err := createWebSocketContext(c, config, selectedProtocol, upgradeData)
			if err != nil {
				return fmt.Errorf("failed to create websocket context: %w", err)
			}

			// 7. Some test doubles do not perform a real upgrade, so preserve the
			// legacy header path only when the context is not already connected.
			if !wsCtx.IsConnected() {
				if err := setupWebSocketHeaders(c, wsKey, selectedProtocol); err != nil {
					return fmt.Errorf("failed to setup websocket headers: %w", err)
				}
			}

			// 8. Call event handler if configured
			if config.OnConnect != nil {
				if err := config.OnConnect(wsCtx); err != nil {
					return fmt.Errorf("websocket connect handler failed: %w", err)
				}
			}

			stopPingLoop := startWebSocketPingLoop(wsCtx, config, &defaultLogger{})
			defer stopPingLoop()

			// 9. Continue with WebSocket context
			return wsCtx.Next()
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
func createWebSocketContext(c Context, config WebSocketConfig, protocol string, upgradeData UpgradeData) (WebSocketContext, error) {
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
	inheritWebSocketRouteState(c, wsCtx)

	// Wrap with enhanced context that includes upgrade data
	enhancedWsCtx := &enhancedWebSocketContext{
		WebSocketContext: wsCtx,
		upgradeData:      upgradeData,
	}

	// Note: protocol parameter will be used by adapter-specific implementations
	_ = protocol // Suppress unused parameter warning for now

	return enhancedWsCtx, nil
}

// getWebSocketFactory returns the appropriate WebSocket factory for the context type
func getWebSocketFactory(c Context) WebSocketContextFactory {
	if factory := globalFactoryRegistry.GetByContext(c); factory != nil {
		return factory
	}
	switch c.(type) {
	case *fiberContext, *fiberV3Context, *httpRouterContext:
		return nil
	}
	// Preserve the historical unknown-context fallback, without native collisions.
	return GetHTTPRouterWebSocketFactory()
}

func inheritWebSocketRouteState(src Context, dst WebSocketContext) {
	switch source := src.(type) {
	case *fiberV3Context:
		target, ok := dst.(*fiberV3WebSocketContext)
		if !ok || target == nil || target.fiberV3Context == nil {
			return
		}
		target.mergeStrategy = source.mergeStrategy
		target.handlers = source.handlers
		target.index = source.index
		target.store = source.store
		target.logger = source.logger
		target.meta = source.meta
		target.SetContext(source.Context())
	case *fiberContext:
		target, ok := dst.(*fiberWebSocketContext)
		if !ok || target == nil || target.fiberContext == nil {
			return
		}
		target.mergeStrategy = source.mergeStrategy
		target.handlers = source.handlers
		target.index = source.index
		target.store = source.store
		target.logger = source.logger
		target.meta = source.meta
		target.SetContext(source.Context())
	case *httpRouterContext:
		target, ok := dst.(*httpRouterWebSocketContext)
		if !ok || target == nil || target.httpRouterContext == nil {
			return
		}
		target.handlers = source.handlers
		target.index = source.index
		target.store = source.store
		target.passLocalsToViews = source.passLocalsToViews
		target.router = source.router
		if source.locals != nil {
			locals := make(ViewContext, len(source.locals))
			maps.Copy(locals, source.locals)
			target.locals = locals
		}
		target.SetContext(source.Context())
	}
}

// WebSocketContextFactory interface for creating adapter-specific WebSocket contexts
type WebSocketContextFactory interface {
	CreateWebSocketContext(c Context, config WebSocketConfig) (WebSocketContext, error)
	SupportsWebSocket() bool
	AdapterName() string
}

// RegisterWebSocketFactory preserves the legacy entry point while all reads
// and writes use the synchronized registry.
func RegisterWebSocketFactory(adapterName string, factory WebSocketContextFactory) {
	key := adapterName
	switch strings.ToLower(adapterName) {
	case "fiber", "fiber-v3", "httprouter":
		key = strings.ToLower(adapterName)
	}
	globalFactoryRegistry.Register(key, factory)
}

func GetFiberWebSocketFactory() WebSocketContextFactory { return globalFactoryRegistry.Get("fiber") }
func GetFiberV3WebSocketFactory() WebSocketContextFactory {
	return globalFactoryRegistry.Get("fiber-v3")
}
func GetHTTPRouterWebSocketFactory() WebSocketContextFactory {
	return globalFactoryRegistry.Get("httprouter")
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

// enhancedWebSocketContext wraps a WebSocket context to provide upgrade data access
type enhancedWebSocketContext struct {
	WebSocketContext
	upgradeData UpgradeData
}

// UpgradeData implements the UpgradeData method to provide access to pre-upgrade data
func (e *enhancedWebSocketContext) UpgradeData(key string) (any, bool) {
	if e.upgradeData == nil {
		return nil, false
	}
	value, exists := e.upgradeData[key]
	return value, exists
}
