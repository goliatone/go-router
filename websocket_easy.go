package router

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
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

// WSTokenValidator interface for WebSocket authentication
// Mirrors the go-auth TokenService.Validate method signature
type WSTokenValidator interface {
	Validate(tokenString string) (WSAuthClaims, error)
}

// WSAuthClaims interface for structured auth claims
// Compatible with go-auth AuthClaims interface
type WSAuthClaims interface {
	Subject() string
	UserID() string
	Role() string
	CanRead(resource string) bool
	CanEdit(resource string) bool
	CanCreate(resource string) bool
	CanDelete(resource string) bool
	HasRole(role string) bool
	IsAtLeast(minRole string) bool
}

// WSAuthContextKey is used to store auth claims in context
type WSAuthContextKey struct{}

// WSAuthClaimsFromContext retrieves auth claims from context
func WSAuthClaimsFromContext(ctx context.Context) (WSAuthClaims, bool) {
	claims, ok := ctx.Value(WSAuthContextKey{}).(WSAuthClaims)
	return claims, ok
}

// WSAuthConfig holds configuration for the WebSocket authentication middleware
type WSAuthConfig struct {
	// TokenValidator is required for token validation
	TokenValidator WSTokenValidator
	// TokenExtractor defines how to extract tokens from WebSocket context
	// Supports query parameters and headers
	TokenExtractor func(ctx context.Context, client WSClient) (string, error)
	// ContextEnricher enriches the context with auth claims after validation
	ContextEnricher func(ctx context.Context, claims WSAuthClaims) context.Context
	// OnAuthFailure handles authentication failures
	OnAuthFailure func(ctx context.Context, client WSClient, err error) error
	// Skip allows bypassing auth for certain connections
	Skip func(ctx context.Context, client WSClient) bool
	// Logger for auth-related logging
	Logger Logger
}

// defaultTokenExtractor checks multiple sources for authentication tokens
func defaultTokenExtractor(ctx context.Context, client WSClient) (string, error) {
	// Priority order: query parameter -> header

	// 1. Check query parameter "token" or "auth_token"
	if token := client.Conn().Query("token"); token != "" {
		return token, nil
	}
	if token := client.Conn().Query("auth_token"); token != "" {
		return token, nil
	}

	// 2. Check Authorization header
	if auth := client.Conn().Header("Authorization"); auth != "" {
		// Handle "Bearer <token>" format
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimSpace(auth[7:]), nil
		}
		return auth, nil
	}

	return "", errors.New("no authentication token found")
}

// defaultContextEnricher adds auth claims to the context
func defaultContextEnricher(ctx context.Context, claims WSAuthClaims) context.Context {
	return context.WithValue(ctx, WSAuthContextKey{}, claims)
}

// defaultAuthFailureHandler closes the connection with appropriate status
func defaultAuthFailureHandler(ctx context.Context, client WSClient, err error) error {
	// Close with 1008 Policy Violation for auth failures
	client.Close(ClosePolicyViolation, "Authentication failed")
	return err
}

// WSAuthConfigDefault provides default configuration for WSAuth middleware
var WSAuthConfigDefault = WSAuthConfig{
	TokenExtractor:  defaultTokenExtractor,
	ContextEnricher: defaultContextEnricher,
	OnAuthFailure:   defaultAuthFailureHandler,
	Logger:          &defaultLogger{},
}

// wsAuthConfigDefault merges user config with defaults
func wsAuthConfigDefault(config ...WSAuthConfig) WSAuthConfig {
	if len(config) == 0 {
		panic("WSAuth: TokenValidator is required")
	}

	cfg := config[0]

	if cfg.TokenValidator == nil {
		panic("WSAuth: TokenValidator is required")
	}
	if cfg.TokenExtractor == nil {
		cfg.TokenExtractor = WSAuthConfigDefault.TokenExtractor
	}
	if cfg.ContextEnricher == nil {
		cfg.ContextEnricher = WSAuthConfigDefault.ContextEnricher
	}
	if cfg.OnAuthFailure == nil {
		cfg.OnAuthFailure = WSAuthConfigDefault.OnAuthFailure
	}
	if cfg.Logger == nil {
		cfg.Logger = WSAuthConfigDefault.Logger
	}

	return cfg
}

// NewWSAuth creates a new WebSocket authentication middleware
func NewWSAuth(config ...WSAuthConfig) WebSocketMiddleware {
	cfg := wsAuthConfigDefault(config...)

	return func(next SimpleWSHandler) SimpleWSHandler {
		return func(ctx context.Context, client WSClient) error {
			// Skip authentication if configured
			if cfg.Skip != nil && cfg.Skip(ctx, client) {
				return next(ctx, client)
			}

			// Extract token from request
			token, err := cfg.TokenExtractor(ctx, client)
			if err != nil {
				cfg.Logger.Warn("WSAuth: token extraction failed: %v", err)
				return cfg.OnAuthFailure(ctx, client, err)
			}

			// Validate token
			claims, err := cfg.TokenValidator.Validate(token)
			if err != nil {
				cfg.Logger.Warn("WSAuth: token validation failed: %v", err)
				return cfg.OnAuthFailure(ctx, client, err)
			}

			// Enrich context with claims
			enrichedCtx := cfg.ContextEnricher(ctx, claims)

			cfg.Logger.Info("WSAuth: authentication successful for user: %s", claims.UserID())

			return next(enrichedCtx, client)
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
