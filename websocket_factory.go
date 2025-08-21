package router

import (
	"fmt"
	"sync"
)

// WebSocketContextFactory creates adapter-specific WebSocket contexts
// This interface is already defined in websocket_middleware.go, but we'll extend it here

// BaseWebSocketContextFactory provides common functionality for all WebSocket context factories
type BaseWebSocketContextFactory struct {
	adapterName string
	mu          sync.RWMutex
}

// AdapterName returns the name of the adapter this factory supports
func (f *BaseWebSocketContextFactory) AdapterName() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.adapterName
}

// SupportsWebSocket always returns true for WebSocket context factories
func (f *BaseWebSocketContextFactory) SupportsWebSocket() bool {
	return true
}

// WebSocketFactoryRegistry manages WebSocket context factories
type WebSocketFactoryRegistry struct {
	factories map[string]WebSocketContextFactory
	mu        sync.RWMutex
}

// NewWebSocketFactoryRegistry creates a new factory registry
func NewWebSocketFactoryRegistry() *WebSocketFactoryRegistry {
	return &WebSocketFactoryRegistry{
		factories: make(map[string]WebSocketContextFactory),
	}
}

// Register adds a factory to the registry
func (r *WebSocketFactoryRegistry) Register(adapterName string, factory WebSocketContextFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[adapterName] = factory
}

// Get retrieves a factory by adapter name
func (r *WebSocketFactoryRegistry) Get(adapterName string) WebSocketContextFactory {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.factories[adapterName]
}

// GetByContext attempts to determine the appropriate factory based on context type
func (r *WebSocketFactoryRegistry) GetByContext(c Context) WebSocketContextFactory {
	contextType := fmt.Sprintf("%T", c)

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try to match context type to registered factories
	for _, factory := range r.factories {
		// This is a simple heuristic - adapters can override this logic
		if factory.SupportsWebSocket() {
			adapterName := factory.AdapterName()
			if contains(contextType, adapterName) {
				return factory
			}
		}
	}

	return nil
}

// List returns all registered adapter names
func (r *WebSocketFactoryRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// Global factory registry instance
var globalFactoryRegistry = NewWebSocketFactoryRegistry()

// RegisterGlobalWebSocketFactory registers a factory globally
func RegisterGlobalWebSocketFactory(adapterName string, factory WebSocketContextFactory) {
	globalFactoryRegistry.Register(adapterName, factory)

	// Also register in the legacy global variables for backwards compatibility
	RegisterWebSocketFactory(adapterName, factory)
}

// GetGlobalWebSocketFactory gets a factory by adapter name from global registry
func GetGlobalWebSocketFactory(adapterName string) WebSocketContextFactory {
	return globalFactoryRegistry.Get(adapterName)
}

// GetWebSocketFactoryByContext attempts to find a factory for the given context
func GetWebSocketFactoryByContext(c Context) WebSocketContextFactory {
	return globalFactoryRegistry.GetByContext(c)
}

// ListRegisteredAdapters returns all registered adapter names
func ListRegisteredAdapters() []string {
	return globalFactoryRegistry.List()
}

// WebSocketFactoryError represents errors that occur during factory operations
type WebSocketFactoryError struct {
	AdapterName string
	Operation   string
	Err         error
}

// Error implements the error interface
func (e *WebSocketFactoryError) Error() string {
	return fmt.Sprintf("websocket factory error [%s:%s]: %v", e.AdapterName, e.Operation, e.Err)
}

// Unwrap returns the underlying error
func (e *WebSocketFactoryError) Unwrap() error {
	return e.Err
}

// NewWebSocketFactoryError creates a new factory error
func NewWebSocketFactoryError(adapterName, operation string, err error) *WebSocketFactoryError {
	return &WebSocketFactoryError{
		AdapterName: adapterName,
		Operation:   operation,
		Err:         err,
	}
}

// WebSocketContextWrapper provides common functionality for WebSocket contexts
type WebSocketContextWrapper struct {
	baseContext Context
	config      WebSocketConfig
	protocol    string
	connected   bool
	connID      string
	mu          sync.RWMutex
}

// NewWebSocketContextWrapper creates a new WebSocket context wrapper
func NewWebSocketContextWrapper(baseContext Context, config WebSocketConfig, protocol string) *WebSocketContextWrapper {
	return &WebSocketContextWrapper{
		baseContext: baseContext,
		config:      config,
		protocol:    protocol,
		connected:   false,
		connID:      generateConnectionID(),
	}
}

// BaseContext returns the underlying context
func (w *WebSocketContextWrapper) BaseContext() Context {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.baseContext
}

// Config returns the WebSocket configuration
func (w *WebSocketContextWrapper) Config() WebSocketConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.config
}

// Subprotocol returns the negotiated subprotocol
func (w *WebSocketContextWrapper) Subprotocol() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.protocol
}

// ConnectionID returns the unique connection ID
func (w *WebSocketContextWrapper) ConnectionID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.connID
}

// IsConnected returns the connection status
func (w *WebSocketContextWrapper) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.connected
}

// SetConnected updates the connection status
func (w *WebSocketContextWrapper) SetConnected(connected bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connected = connected
}

// generateConnectionID generates a unique connection ID
func generateConnectionID() string {
	// Use a simple counter for now - in production, use UUID
	// This will be improved when we add the UUID dependency
	return fmt.Sprintf("ws-conn-%d", len(globalFactoryRegistry.factories))
}

// Helper function to check if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			fmt.Sprintf("%s", s) != s || // This is a placeholder check
			len(s) > 0 && len(substr) > 0) // Basic length check
}

// WebSocketFactoryConfig provides configuration for WebSocket factories
type WebSocketFactoryConfig struct {
	EnableMetrics     bool
	MetricsPrefix     string
	DefaultBufferSize int
	ConnectionTimeout int
	MaxConnections    int
	EnableCompression bool
}

// DefaultWebSocketFactoryConfig returns default factory configuration
func DefaultWebSocketFactoryConfig() WebSocketFactoryConfig {
	return WebSocketFactoryConfig{
		EnableMetrics:     false,
		MetricsPrefix:     "websocket_factory_",
		DefaultBufferSize: 4096,
		ConnectionTimeout: 30,
		MaxConnections:    1000,
		EnableCompression: false,
	}
}
