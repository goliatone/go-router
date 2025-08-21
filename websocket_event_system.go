package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// EventMessage represents a structured event message
type EventMessage struct {
	ID        string                 `json:"id,omitempty"`
	Type      string                 `json:"type"`
	Namespace string                 `json:"namespace,omitempty"`
	Data      interface{}            `json:"data,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	AckID     string                 `json:"ack_id,omitempty"`
}

// EventRouter manages event routing with namespaces
type EventRouter struct {
	namespaces  map[string]*EventNamespace
	namespaceMu sync.RWMutex

	// Global event handlers
	globalHandlers  map[string][]TypedEventHandler
	globalHandlerMu sync.RWMutex

	// Middleware chain
	middleware []EventMiddleware

	// Event history
	history        *EventHistory
	historyEnabled bool

	// Configuration
	config EventRouterConfig
}

// EventRouterConfig contains configuration for the event router
type EventRouterConfig struct {
	// Enable event history
	EnableHistory bool

	// Maximum history size
	MaxHistorySize int

	// Event TTL for history
	HistoryTTL time.Duration

	// Enable event validation
	EnableValidation bool

	// Default timeout for acknowledgments
	AckTimeout time.Duration

	// Enable event batching
	EnableBatching bool

	// Batch size and interval
	BatchSize     int
	BatchInterval time.Duration
}

// EventNamespace represents an isolated event space
type EventNamespace struct {
	name      string
	router    *EventRouter
	handlers  map[string][]TypedEventHandler
	handlerMu sync.RWMutex

	// Namespace-specific middleware
	middleware []EventMiddleware

	// Authorization
	authFunc NamespaceAuthFunc

	// Clients in this namespace
	clients   map[string]WSClient
	clientsMu sync.RWMutex
}

// TypedEventHandler is a type-safe event handler
type TypedEventHandler interface {
	Handle(ctx context.Context, client WSClient, event *EventMessage) error
	EventType() string
	Validate(data interface{}) error
}

// GenericEventHandler handles events with a generic function
type GenericEventHandler struct {
	Type      string
	Handler   func(context.Context, WSClient, *EventMessage) error
	Validator func(interface{}) error
}

func (h *GenericEventHandler) Handle(ctx context.Context, client WSClient, event *EventMessage) error {
	return h.Handler(ctx, client, event)
}

func (h *GenericEventHandler) EventType() string {
	return h.Type
}

func (h *GenericEventHandler) Validate(data interface{}) error {
	if h.Validator != nil {
		return h.Validator(data)
	}
	return nil
}

// TypedHandler creates a type-safe handler for a specific data type
func TypedHandler[T any](eventType string, handler func(context.Context, WSClient, T) error) TypedEventHandler {
	return &typedHandlerImpl[T]{
		eventType: eventType,
		handler:   handler,
	}
}

type typedHandlerImpl[T any] struct {
	eventType string
	handler   func(context.Context, WSClient, T) error
}

func (h *typedHandlerImpl[T]) Handle(ctx context.Context, client WSClient, event *EventMessage) error {
	var data T

	// Convert event data to the expected type
	if event.Data != nil {
		// If data is already the correct type
		if typedData, ok := event.Data.(T); ok {
			data = typedData
		} else {
			// Try to convert via JSON marshaling
			jsonData, err := json.Marshal(event.Data)
			if err != nil {
				return fmt.Errorf("failed to marshal event data: %w", err)
			}

			if err := json.Unmarshal(jsonData, &data); err != nil {
				return fmt.Errorf("failed to unmarshal event data to type %T: %w", data, err)
			}
		}
	}

	return h.handler(ctx, client, data)
}

func (h *typedHandlerImpl[T]) EventType() string {
	return h.eventType
}

func (h *typedHandlerImpl[T]) Validate(data interface{}) error {
	// Basic type validation
	var zero T
	expectedType := reflect.TypeOf(zero)
	actualType := reflect.TypeOf(data)

	if expectedType != actualType {
		return fmt.Errorf("type mismatch: expected %v, got %v", expectedType, actualType)
	}

	return nil
}

// EventMiddleware processes events before they reach handlers
type EventMiddleware func(ctx context.Context, client WSClient, event *EventMessage, next EventMiddlewareNext) error

// EventMiddlewareNext is the next function in the middleware chain
type EventMiddlewareNext func(ctx context.Context, client WSClient, event *EventMessage) error

// NamespaceAuthFunc authorizes access to a namespace
type NamespaceAuthFunc func(ctx context.Context, client WSClient) error

// NewEventRouter creates a new event router
func NewEventRouter(config EventRouterConfig) *EventRouter {
	if config.AckTimeout == 0 {
		config.AckTimeout = 30 * time.Second
	}
	if config.MaxHistorySize == 0 {
		config.MaxHistorySize = 1000
	}
	if config.HistoryTTL == 0 {
		config.HistoryTTL = 1 * time.Hour
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BatchInterval == 0 {
		config.BatchInterval = 100 * time.Millisecond
	}

	router := &EventRouter{
		namespaces:     make(map[string]*EventNamespace),
		globalHandlers: make(map[string][]TypedEventHandler),
		config:         config,
		historyEnabled: config.EnableHistory,
	}

	if config.EnableHistory {
		router.history = NewEventHistory(config.MaxHistorySize, config.HistoryTTL)
	}

	return router
}

// Namespace returns or creates a namespace
func (r *EventRouter) Namespace(name string) *EventNamespace {
	r.namespaceMu.Lock()
	defer r.namespaceMu.Unlock()

	if ns, exists := r.namespaces[name]; exists {
		return ns
	}

	ns := &EventNamespace{
		name:     name,
		router:   r,
		handlers: make(map[string][]TypedEventHandler),
		clients:  make(map[string]WSClient),
	}

	r.namespaces[name] = ns
	return ns
}

// On registers a global event handler
func (r *EventRouter) On(eventType string, handler TypedEventHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	r.globalHandlerMu.Lock()
	defer r.globalHandlerMu.Unlock()

	r.globalHandlers[eventType] = append(r.globalHandlers[eventType], handler)
	return nil
}

// Use adds middleware to the global chain
func (r *EventRouter) Use(middleware EventMiddleware) {
	r.middleware = append(r.middleware, middleware)
}

// RouteEvent routes an event through the system
func (r *EventRouter) RouteEvent(ctx context.Context, client WSClient, event *EventMessage) error {
	// Add timestamp if not present
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Store in history if enabled
	if r.historyEnabled && r.history != nil {
		r.history.Add(event)
	}

	// Apply global middleware chain
	handler := r.createHandlerChain(event)

	return handler(ctx, client, event)
}

func (r *EventRouter) createHandlerChain(event *EventMessage) EventMiddlewareNext {
	// Final handler that routes to appropriate handlers
	finalHandler := func(ctx context.Context, client WSClient, event *EventMessage) error {
		// Route to namespace handlers if namespace is specified
		if event.Namespace != "" {
			if ns := r.getNamespace(event.Namespace); ns != nil {
				return ns.handleEvent(ctx, client, event)
			}
		}

		// Route to global handlers
		return r.handleGlobalEvent(ctx, client, event)
	}

	// Build middleware chain in reverse order
	handler := finalHandler
	for i := len(r.middleware) - 1; i >= 0; i-- {
		middleware := r.middleware[i]
		currentHandler := handler
		handler = func(ctx context.Context, client WSClient, event *EventMessage) error {
			return middleware(ctx, client, event, currentHandler)
		}
	}

	return handler
}

func (r *EventRouter) handleGlobalEvent(ctx context.Context, client WSClient, event *EventMessage) error {
	r.globalHandlerMu.RLock()
	handlers := r.globalHandlers[event.Type]
	r.globalHandlerMu.RUnlock()

	if len(handlers) == 0 {
		return nil // No handlers for this event
	}

	var lastErr error
	for _, handler := range handlers {
		// Validate if enabled
		if r.config.EnableValidation {
			if err := handler.Validate(event.Data); err != nil {
				lastErr = fmt.Errorf("validation failed: %w", err)
				continue
			}
		}

		if err := handler.Handle(ctx, client, event); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func (r *EventRouter) getNamespace(name string) *EventNamespace {
	r.namespaceMu.RLock()
	defer r.namespaceMu.RUnlock()
	return r.namespaces[name]
}

// GetHistory returns event history
func (r *EventRouter) GetHistory(filter EventHistoryFilter) []*EventMessage {
	if !r.historyEnabled || r.history == nil {
		return nil
	}

	return r.history.Get(filter)
}

// Namespace methods

// On registers an event handler in this namespace
func (ns *EventNamespace) On(eventType string, handler TypedEventHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	ns.handlerMu.Lock()
	defer ns.handlerMu.Unlock()

	ns.handlers[eventType] = append(ns.handlers[eventType], handler)
	return nil
}

// Use adds middleware to this namespace
func (ns *EventNamespace) Use(middleware EventMiddleware) {
	ns.middleware = append(ns.middleware, middleware)
}

// SetAuth sets the authorization function for this namespace
func (ns *EventNamespace) SetAuth(authFunc NamespaceAuthFunc) {
	ns.authFunc = authFunc
}

// Join adds a client to this namespace
func (ns *EventNamespace) Join(ctx context.Context, client WSClient) error {
	// Check authorization
	if ns.authFunc != nil {
		if err := ns.authFunc(ctx, client); err != nil {
			return fmt.Errorf("authorization failed: %w", err)
		}
	}

	ns.clientsMu.Lock()
	defer ns.clientsMu.Unlock()

	ns.clients[client.ID()] = client
	return nil
}

// Leave removes a client from this namespace
func (ns *EventNamespace) Leave(client WSClient) {
	ns.clientsMu.Lock()
	defer ns.clientsMu.Unlock()

	delete(ns.clients, client.ID())
}

// Emit sends an event to all clients in this namespace
func (ns *EventNamespace) Emit(ctx context.Context, event *EventMessage) error {
	event.Namespace = ns.name

	ns.clientsMu.RLock()
	clients := make([]WSClient, 0, len(ns.clients))
	for _, client := range ns.clients {
		clients = append(clients, client)
	}
	ns.clientsMu.RUnlock()

	var lastErr error
	for _, client := range clients {
		if err := client.EmitWithContext(ctx, event.Type, event); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func (ns *EventNamespace) handleEvent(ctx context.Context, client WSClient, event *EventMessage) error {
	// Apply namespace middleware chain
	handler := ns.createHandlerChain(event)
	return handler(ctx, client, event)
}

func (ns *EventNamespace) createHandlerChain(event *EventMessage) EventMiddlewareNext {
	// Final handler that executes registered handlers
	finalHandler := func(ctx context.Context, client WSClient, event *EventMessage) error {
		ns.handlerMu.RLock()
		handlers := ns.handlers[event.Type]
		ns.handlerMu.RUnlock()

		if len(handlers) == 0 {
			// Fall back to global handlers
			return ns.router.handleGlobalEvent(ctx, client, event)
		}

		var lastErr error
		for _, handler := range handlers {
			// Validate if enabled
			if ns.router.config.EnableValidation {
				if err := handler.Validate(event.Data); err != nil {
					lastErr = fmt.Errorf("validation failed: %w", err)
					continue
				}
			}

			if err := handler.Handle(ctx, client, event); err != nil {
				lastErr = err
			}
		}

		return lastErr
	}

	// Build middleware chain in reverse order
	handler := finalHandler
	for i := len(ns.middleware) - 1; i >= 0; i-- {
		middleware := ns.middleware[i]
		currentHandler := handler
		handler = func(ctx context.Context, client WSClient, event *EventMessage) error {
			return middleware(ctx, client, event, currentHandler)
		}
	}

	return handler
}

// ParseEventNamespace extracts namespace from event type (e.g., "chat:message" -> "chat", "message")
func ParseEventNamespace(eventType string) (namespace, event string) {
	parts := strings.SplitN(eventType, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", eventType
}

// Built-in middleware

// LoggingMiddleware logs all events
func LoggingMiddleware() EventMiddleware {
	return func(ctx context.Context, client WSClient, event *EventMessage, next EventMiddlewareNext) error {
		// Log event (implementation would use actual logger)
		fmt.Printf("[EVENT] Client=%s Type=%s Namespace=%s\n", client.ID(), event.Type, event.Namespace)
		return next(ctx, client, event)
	}
}

// ValidationMiddleware validates event data against schemas
func ValidationMiddleware(schemas map[string]interface{}) EventMiddleware {
	return func(ctx context.Context, client WSClient, event *EventMessage, next EventMiddlewareNext) error {
		// Validate against schema if exists
		if schema, exists := schemas[event.Type]; exists {
			// Perform validation (simplified)
			_ = schema
		}
		return next(ctx, client, event)
	}
}

// RateLimitMiddleware limits events per client
func RateLimitMiddleware(eventsPerSecond int) EventMiddleware {
	// Track rate limits per client
	type clientRate struct {
		count     int
		resetTime time.Time
		mu        sync.Mutex
	}

	rates := make(map[string]*clientRate)
	ratesMu := sync.RWMutex{}

	return func(ctx context.Context, client WSClient, event *EventMessage, next EventMiddlewareNext) error {
		ratesMu.RLock()
		rate, exists := rates[client.ID()]
		ratesMu.RUnlock()

		if !exists {
			ratesMu.Lock()
			rate = &clientRate{
				resetTime: time.Now().Add(time.Second),
			}
			rates[client.ID()] = rate
			ratesMu.Unlock()
		}

		rate.mu.Lock()
		defer rate.mu.Unlock()

		now := time.Now()
		if now.After(rate.resetTime) {
			rate.count = 0
			rate.resetTime = now.Add(time.Second)
		}

		rate.count++
		if rate.count > eventsPerSecond {
			return errors.New("rate limit exceeded")
		}

		return next(ctx, client, event)
	}
}

// AuthorizationMiddleware checks event permissions
func AuthorizationMiddleware(authFunc func(context.Context, WSClient, *EventMessage) error) EventMiddleware {
	return func(ctx context.Context, client WSClient, event *EventMessage, next EventMiddlewareNext) error {
		if err := authFunc(ctx, client, event); err != nil {
			return fmt.Errorf("authorization failed: %w", err)
		}
		return next(ctx, client, event)
	}
}

// TransformMiddleware transforms event data
func TransformMiddleware(transformer func(*EventMessage) error) EventMiddleware {
	return func(ctx context.Context, client WSClient, event *EventMessage, next EventMiddlewareNext) error {
		if err := transformer(event); err != nil {
			return fmt.Errorf("transform failed: %w", err)
		}
		return next(ctx, client, event)
	}
}
