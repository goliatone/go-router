package router

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// EasyWebSocket creates a simple WebSocket handler with minimal configuration
func EasyWebSocket(handler func(context.Context, WSClient) error) func(Context) error {
	hub := NewWSHub()

	// Set up the connection handler
	hub.OnConnect(func(ctx context.Context, client WSClient, _ any) error {
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
			// TODO: add authentication logic here
			// For now, just pass through
			return next(ctx, client)
		}
	}
}

// WSLoggerConfig holds configuration for the WebSocket logging middleware
type WSLoggerConfig struct {
	// Logger is the logger instance.
	Logger Logger
	// Skip allows skipping the middleware based on the WebSocket context.
	Skip func(c WebSocketContext) bool
	// Formatter is a function to format the log string.
	Formatter func(WSClient, time.Time, time.Time) string
}

// WSLoggerConfigDefault provides default configuration for WSLogger middleware
var WSLoggerConfigDefault = WSLoggerConfig{
	Logger: &defaultLogger{},
	Skip:   nil,
	Formatter: func(client WSClient, start, stop time.Time) string {
		return fmt.Sprintf("[WS] %s | %s | %v | %s",
			client.ID(),
			client.Conn().RemoteAddr(),
			stop.Sub(start),
			client.Conn().Path(),
		)
	},
}

// wsLoggerConfigDefault merges user config with defaults
func wsLoggerConfigDefault(config ...WSLoggerConfig) WSLoggerConfig {
	if len(config) == 0 {
		return WSLoggerConfigDefault
	}
	cfg := config[0]

	if cfg.Logger == nil {
		cfg.Logger = WSLoggerConfigDefault.Logger
	}
	if cfg.Formatter == nil {
		cfg.Formatter = WSLoggerConfigDefault.Formatter
	}

	return cfg
}

// NewWSLogger returns a WebSocket logging middleware with the specified configuration
func NewWSLogger(config ...WSLoggerConfig) WebSocketMiddleware {
	cfg := wsLoggerConfigDefault(config...)

	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			if cfg.Skip != nil && cfg.Skip(client.Conn()) {
				return next(ctx, client)
			}

			start := time.Now()
			err := next(ctx, client)
			stop := time.Now()

			cfg.Logger.Info(cfg.Formatter(client, start, stop))

			return err
		}
	}
}

// WSConnectionMetrics represents the metrics data collected by the middleware
type WSConnectionMetrics struct {
	ClientID           string        `json:"client_id"`
	RemoteAddr         string        `json:"remote_addr"`
	ConnectionStart    time.Time     `json:"connection_start"`
	ConnectionEnd      time.Time     `json:"connection_end"`
	ConnectionDuration time.Duration `json:"connection_duration"`
	Path               string        `json:"path"`
	Error              string        `json:"error,omitempty"`
}

// WSMetricsSink defines the interface for metrics collection
type WSMetricsSink interface {
	// RecordConnection is called when a WebSocket connection completes
	RecordConnection(metrics WSConnectionMetrics)
}

// WSMetricsConfig holds configuration for the WebSocket metrics middleware
type WSMetricsConfig struct {
	// Sink is the metrics sink where collected metrics will be sent
	Sink WSMetricsSink
	// Skip allows skipping the middleware based on the WebSocket context
	Skip func(c WebSocketContext) bool
}

// defaultMetricsSink is a no-op sink used as default
type defaultMetricsSink struct{}

func (d *defaultMetricsSink) RecordConnection(metrics WSConnectionMetrics) {
	// No-op implementation - metrics are discarded
}

// WSMetricsConfigDefault provides default configuration for WSMetrics middleware
var WSMetricsConfigDefault = WSMetricsConfig{
	Sink: &defaultMetricsSink{},
	Skip: nil,
}

// wsMetricsConfigDefault merges user config with defaults
func wsMetricsConfigDefault(config ...WSMetricsConfig) WSMetricsConfig {
	if len(config) == 0 {
		return WSMetricsConfigDefault
	}
	cfg := config[0]

	if cfg.Sink == nil {
		cfg.Sink = WSMetricsConfigDefault.Sink
	}

	return cfg
}

// NewWSMetrics returns a WebSocket metrics middleware with the specified configuration
func NewWSMetrics(config ...WSMetricsConfig) WebSocketMiddleware {
	cfg := wsMetricsConfigDefault(config...)

	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			if cfg.Skip != nil && cfg.Skip(client.Conn()) {
				return next(ctx, client)
			}

			start := time.Now()
			err := next(ctx, client)
			end := time.Now()

			// Collect metrics
			metrics := WSConnectionMetrics{
				ClientID:           client.ID(),
				RemoteAddr:         client.Conn().RemoteAddr(),
				ConnectionStart:    start,
				ConnectionEnd:      end,
				ConnectionDuration: end.Sub(start),
				Path:               client.Conn().Path(),
			}

			if err != nil {
				metrics.Error = err.Error()
			}

			// Send metrics to sink
			cfg.Sink.RecordConnection(metrics)

			return err
		}
	}
}

// WSRecoverConfig holds configuration for the WebSocket panic recovery middleware
type WSRecoverConfig struct {
	// Logger is the logger instance.
	Logger Logger
	// Skip allows skipping the middleware based on the WebSocket context.
	Skip func(c WebSocketContext) bool
	// StackSize sets the buffer size for capturing stack traces (default: 4KB)
	StackSize int
	// EnableStackTrace determines if stack traces should be logged (default: true)
	EnableStackTrace bool
}

// WSRecoverConfigDefault provides default configuration for WSRecover middleware
var WSRecoverConfigDefault = WSRecoverConfig{
	Logger:           &defaultLogger{},
	Skip:             nil,
	StackSize:        4 << 10, // 4KB
	EnableStackTrace: true,
}

// wsRecoverConfigDefault merges user config with defaults
func wsRecoverConfigDefault(config ...WSRecoverConfig) WSRecoverConfig {
	if len(config) == 0 {
		return WSRecoverConfigDefault
	}
	cfg := config[0]

	if cfg.Logger == nil {
		cfg.Logger = WSRecoverConfigDefault.Logger
	}
	if cfg.StackSize <= 0 {
		cfg.StackSize = WSRecoverConfigDefault.StackSize
	}

	return cfg
}

// NewWSRecover returns a WebSocket panic recovery middleware with the specified configuration
func NewWSRecover(config ...WSRecoverConfig) WebSocketMiddleware {
	cfg := wsRecoverConfigDefault(config...)

	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			defer func() {
				if r := recover(); r != nil {
					if cfg.Skip != nil && cfg.Skip(client.Conn()) {
						return
					}

					// Log the panic with stack trace
					if cfg.EnableStackTrace {
						stack := make([]byte, cfg.StackSize)
						length := runtime.Stack(stack, false)
						cfg.Logger.Error("WebSocket panic recovered: %v\nClient ID: %s\nRemote Address: %s\nStack trace:\n%s",
							r, client.ID(), client.Conn().RemoteAddr(), string(stack[:length]))
					} else {
						cfg.Logger.Error("WebSocket panic recovered: %v (Client: %s, Remote: %s)",
							r, client.ID(), client.Conn().RemoteAddr())
					}

					// Close the connection with internal server error
					client.Close(CloseInternalServerErr, "internal error")
				}
			}()
			return next(ctx, client)
		}
	}
}

// WSRateLimitStore defines the interface for storing rate limit data
type WSRateLimitStore interface {
	// GetLimiter returns a rate limiter for the given client identifier
	GetLimiter(clientID string) *rate.Limiter
	// CleanupExpired removes expired limiters to prevent memory leaks
	CleanupExpired()
}

// WSInMemoryRateLimitStore provides an in-memory implementation of WSRateLimitStore
type WSInMemoryRateLimitStore struct {
	mu          sync.RWMutex
	limiters    map[string]*rateLimiterEntry
	rateLimit   rate.Limit
	burstLimit  int
	cleanupDone chan struct{}
	cleanupOnce sync.Once
}

type rateLimiterEntry struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

// NewWSInMemoryRateLimitStore creates a new in-memory rate limit store
func NewWSInMemoryRateLimitStore(rateLimit rate.Limit, burstLimit int) *WSInMemoryRateLimitStore {
	store := &WSInMemoryRateLimitStore{
		limiters:    make(map[string]*rateLimiterEntry),
		rateLimit:   rateLimit,
		burstLimit:  burstLimit,
		cleanupDone: make(chan struct{}),
	}

	// Start cleanup goroutine
	go store.cleanupLoop()

	return store
}

func (s *WSInMemoryRateLimitStore) GetLimiter(clientID string) *rate.Limiter {
	s.mu.RLock()
	entry, exists := s.limiters[clientID]
	s.mu.RUnlock()

	if exists {
		entry.lastAccess = time.Now()
		return entry.limiter
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check in case another goroutine created it
	if entry, exists = s.limiters[clientID]; exists {
		entry.lastAccess = time.Now()
		return entry.limiter
	}

	// Create new limiter
	limiter := rate.NewLimiter(s.rateLimit, s.burstLimit)
	s.limiters[clientID] = &rateLimiterEntry{
		limiter:    limiter,
		lastAccess: time.Now(),
	}

	return limiter
}

func (s *WSInMemoryRateLimitStore) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	expiry := time.Now().Add(-time.Hour) // Clean up entries older than 1 hour
	for clientID, entry := range s.limiters {
		if entry.lastAccess.Before(expiry) {
			delete(s.limiters, clientID)
		}
	}
}

func (s *WSInMemoryRateLimitStore) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.CleanupExpired()
		case <-s.cleanupDone:
			return
		}
	}
}

// Close stops the cleanup goroutine
func (s *WSInMemoryRateLimitStore) Close() {
	s.cleanupOnce.Do(func() {
		close(s.cleanupDone)
	})
}

// WSRateLimitConfig holds configuration for the WebSocket rate limiting middleware
type WSRateLimitConfig struct {
	// Store is the rate limit data store
	Store WSRateLimitStore
	// Skip allows skipping the middleware based on the WebSocket context
	Skip func(c WebSocketContext) bool
	// KeyFunc extracts the rate limit key from the client (defaults to client ID)
	KeyFunc func(client WSClient) string
	// OnRateLimited is called when a client is rate limited
	OnRateLimited func(client WSClient)
}

// WSRateLimitConfigDefault provides default configuration for WSRateLimit middleware
var WSRateLimitConfigDefault = WSRateLimitConfig{
	Store: NewWSInMemoryRateLimitStore(rate.Limit(10), 1), // 10 requests per second, burst of 1
	Skip:  nil,
	KeyFunc: func(client WSClient) string {
		return client.ID()
	},
	OnRateLimited: nil,
}

// wsRateLimitConfigDefault merges user config with defaults
func wsRateLimitConfigDefault(config ...WSRateLimitConfig) WSRateLimitConfig {
	if len(config) == 0 {
		return WSRateLimitConfigDefault
	}
	cfg := config[0]

	if cfg.Store == nil {
		cfg.Store = WSRateLimitConfigDefault.Store
	}
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = WSRateLimitConfigDefault.KeyFunc
	}

	return cfg
}

// NewWSRateLimit returns a WebSocket rate limiting middleware with the specified configuration
func NewWSRateLimit(config ...WSRateLimitConfig) WebSocketMiddleware {
	cfg := wsRateLimitConfigDefault(config...)

	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			if cfg.Skip != nil && cfg.Skip(client.Conn()) {
				return next(ctx, client)
			}

			// Get rate limiter for this client
			clientKey := cfg.KeyFunc(client)
			limiter := cfg.Store.GetLimiter(clientKey)

			// Check if request is allowed
			if !limiter.Allow() {
				// Rate limit exceeded
				if cfg.OnRateLimited != nil {
					cfg.OnRateLimited(client)
				}

				// Close connection with policy violation status
				return client.Close(ClosePolicyViolation, "rate limit exceeded")
			}

			return next(ctx, client)
		}
	}
}

// ChainWSMiddleware chains multiple WebSocket middlewares.
// Middleware is executed in an "outside-in" order, meaning the first middleware
// in the list becomes the outermost layer and will be executed first on the way in
// and last on the way out. For example: ChainWSMiddleware(WSRecover(), WSLogger(), WSAuth(), ...)
// will execute WSRecover first, then WSLogger, then WSAuth when processing a request.
func ChainWSMiddleware(middlewares ...WebSocketMiddleware) WebSocketMiddleware {
	return func(next SimpleWSHandler) SimpleWSHandler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}
