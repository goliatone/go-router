package router

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// WebSocketSecurityPolicy defines security policies for WebSocket connections
type WebSocketSecurityPolicy struct {
	// Origin validation
	AllowedOrigins      []string
	DisallowedOrigins   []string
	RequireOriginHeader bool
	SameOriginOnly      bool
	CustomOriginChecker func(origin string) bool

	// Protocol validation
	AllowedProtocols      []string
	DisallowedProtocols   []string
	CustomProtocolChecker func(protocol string) bool

	// Header validation
	RequiredHeaders       map[string]string
	DisallowedHeaders     []string
	CustomHeaderValidator func(headers map[string]string) bool

	// Rate limiting
	EnableRateLimit     bool
	MaxConnectionsPerIP int
	ConnectionRateLimit int // connections per minute

	// Security features
	RequireSecureOrigin  bool // Only allow HTTPS origins
	AllowLocalhostOrigin bool // Allow localhost for development
	StrictHostValidation bool // Validate Host header matches expected values
	AllowedHosts         []string
}

// DefaultWebSocketSecurityPolicy returns a secure default policy
func DefaultWebSocketSecurityPolicy() WebSocketSecurityPolicy {
	return WebSocketSecurityPolicy{
		AllowedOrigins:        []string{}, // Empty means same-origin only
		DisallowedOrigins:     []string{},
		RequireOriginHeader:   true,
		SameOriginOnly:        true,
		CustomOriginChecker:   nil,
		AllowedProtocols:      []string{},
		DisallowedProtocols:   []string{},
		CustomProtocolChecker: nil,
		RequiredHeaders:       make(map[string]string),
		DisallowedHeaders:     []string{},
		CustomHeaderValidator: nil,
		EnableRateLimit:       false,
		MaxConnectionsPerIP:   10,
		ConnectionRateLimit:   60,
		RequireSecureOrigin:   false, // Set to true in production
		AllowLocalhostOrigin:  true,  // Allow for development
		StrictHostValidation:  false,
		AllowedHosts:          []string{},
	}
}

// ProductionWebSocketSecurityPolicy returns a strict security policy for production
func ProductionWebSocketSecurityPolicy() WebSocketSecurityPolicy {
	return WebSocketSecurityPolicy{
		AllowedOrigins:        []string{}, // Must be configured per application
		DisallowedOrigins:     []string{},
		RequireOriginHeader:   true,
		SameOriginOnly:        false, // Allow configured origins
		CustomOriginChecker:   nil,
		AllowedProtocols:      []string{},
		DisallowedProtocols:   []string{},
		CustomProtocolChecker: nil,
		RequiredHeaders:       make(map[string]string),
		DisallowedHeaders:     []string{"x-debug", "x-test"},
		CustomHeaderValidator: nil,
		EnableRateLimit:       true,
		MaxConnectionsPerIP:   5,
		ConnectionRateLimit:   30,
		RequireSecureOrigin:   true,  // Only HTTPS origins
		AllowLocalhostOrigin:  false, // No localhost in production
		StrictHostValidation:  true,
		AllowedHosts:          []string{}, // Must be configured per application
	}
}

// ValidateWebSocketSecurity performs comprehensive security validation
func ValidateWebSocketSecurity(c Context, policy WebSocketSecurityPolicy) error {
	// 1. Validate origin
	if err := validateOriginSecurity(c, policy); err != nil {
		return err
	}

	// 2. Validate protocols
	if err := validateProtocolSecurity(c, policy); err != nil {
		return err
	}

	// 3. Validate headers
	if err := validateHeaderSecurity(c, policy); err != nil {
		return err
	}

	// 4. Validate host (if strict validation enabled)
	if policy.StrictHostValidation {
		if err := validateHostSecurity(c, policy); err != nil {
			return err
		}
	}

	return nil
}

// validateOriginSecurity validates the Origin header according to policy
func validateOriginSecurity(c Context, policy WebSocketSecurityPolicy) error {
	origin := c.Header("Origin")

	// Check if origin header is required
	if policy.RequireOriginHeader && origin == "" {
		return NewWebSocketSecurityError("origin_required", "Origin header is required")
	}

	// If same-origin only, validate against request host
	if policy.SameOriginOnly && origin != "" {
		if !isSameOrigin(c) {
			return NewWebSocketSecurityError("origin_same_origin", "Same-origin policy violation")
		}
	}

	// Check secure origin requirement
	if policy.RequireSecureOrigin && origin != "" {
		originURL, err := url.Parse(origin)
		if err != nil {
			return NewWebSocketSecurityError("origin_invalid", "Invalid origin URL")
		}

		if originURL.Scheme != "https" {
			// Allow localhost for development if configured
			if policy.AllowLocalhostOrigin && isLocalhostOrigin(originURL) {
				// Localhost is allowed, continue
			} else {
				return NewWebSocketSecurityError("origin_insecure", "Only HTTPS origins are allowed")
			}
		}
	}

	// Check disallowed origins
	for _, disallowed := range policy.DisallowedOrigins {
		if matchesOriginPattern(origin, disallowed) {
			return NewWebSocketSecurityError("origin_disallowed", "Origin is explicitly disallowed")
		}
	}

	// Check allowed origins (if specified)
	if len(policy.AllowedOrigins) > 0 {
		allowed := false
		for _, allowedOrigin := range policy.AllowedOrigins {
			if allowedOrigin == "*" || matchesOriginPattern(origin, allowedOrigin) {
				allowed = true
				break
			}
		}
		if !allowed {
			return NewWebSocketSecurityError("origin_not_allowed", "Origin is not in the allowed list")
		}
	}

	// Custom origin validation
	if policy.CustomOriginChecker != nil && origin != "" {
		if !policy.CustomOriginChecker(origin) {
			return NewWebSocketSecurityError("origin_custom_check", "Origin failed custom validation")
		}
	}

	return nil
}

// validateProtocolSecurity validates WebSocket subprotocols
func validateProtocolSecurity(c Context, policy WebSocketSecurityPolicy) error {
	requestedProtocols := c.Header(WebSocketProtocol)
	if requestedProtocols == "" {
		return nil // No protocols requested
	}

	protocols := strings.Split(requestedProtocols, ",")
	for _, protocol := range protocols {
		protocol = strings.TrimSpace(protocol)

		// Check disallowed protocols
		for _, disallowed := range policy.DisallowedProtocols {
			if protocol == disallowed {
				return NewWebSocketSecurityError("protocol_disallowed", "Protocol is explicitly disallowed")
			}
		}

		// Check allowed protocols (if specified)
		if len(policy.AllowedProtocols) > 0 {
			allowed := false
			for _, allowedProtocol := range policy.AllowedProtocols {
				if protocol == allowedProtocol {
					allowed = true
					break
				}
			}
			if !allowed {
				return NewWebSocketSecurityError("protocol_not_allowed", "Protocol is not in the allowed list")
			}
		}

		// Custom protocol validation
		if policy.CustomProtocolChecker != nil {
			if !policy.CustomProtocolChecker(protocol) {
				return NewWebSocketSecurityError("protocol_custom_check", "Protocol failed custom validation")
			}
		}
	}

	return nil
}

// validateHeaderSecurity validates request headers according to policy
func validateHeaderSecurity(c Context, policy WebSocketSecurityPolicy) error {
	// Check for required headers
	for headerName, expectedValue := range policy.RequiredHeaders {
		actualValue := c.Header(headerName)
		if actualValue == "" {
			return NewWebSocketSecurityError("header_required", "Required header is missing: "+headerName)
		}
		if expectedValue != "" && actualValue != expectedValue {
			return NewWebSocketSecurityError("header_value_mismatch", "Header value does not match expected: "+headerName)
		}
	}

	// Check for disallowed headers
	for _, disallowedHeader := range policy.DisallowedHeaders {
		if c.Header(disallowedHeader) != "" {
			return NewWebSocketSecurityError("header_disallowed", "Disallowed header present: "+disallowedHeader)
		}
	}

	// Custom header validation
	if policy.CustomHeaderValidator != nil {
		headers := extractHeaders(c)
		if !policy.CustomHeaderValidator(headers) {
			return NewWebSocketSecurityError("header_custom_check", "Headers failed custom validation")
		}
	}

	return nil
}

// validateHostSecurity validates the Host header
func validateHostSecurity(c Context, policy WebSocketSecurityPolicy) error {
	host := requestHost(c)
	if host == "" {
		return NewWebSocketSecurityError("host_required", "Host header is required")
	}

	scheme := requestScheme(c)
	normalizedHost, normalizedPort := normalizeHostAndPort(host, scheme)
	if normalizedHost == "" {
		return NewWebSocketSecurityError("host_invalid", "Host header contains invalid characters")
	}
	if strings.Contains(normalizedHost, "..") || strings.Contains(normalizedHost, "//") {
		return NewWebSocketSecurityError("host_invalid", "Host header contains invalid characters")
	}

	if len(policy.AllowedHosts) == 0 {
		return NewWebSocketSecurityError("host_not_allowed", "Allowed hosts must be configured when strict host validation is enabled")
	}

	for _, allowed := range policy.AllowedHosts {
		if matchesHostPolicy(host, normalizedHost, normalizedPort, scheme, allowed) {
			return nil
		}
	}

	return NewWebSocketSecurityError("host_not_allowed", "Host header is not in the allowed list")
}

func matchesHostPolicy(rawHost, normalizedHost, normalizedPort, scheme, allowed string) bool {
	allowed = strings.TrimSpace(allowed)
	if allowed == "" {
		return false
	}

	if strings.Contains(allowed, "://") {
		allowedURL, err := url.Parse(allowed)
		if err != nil {
			return false
		}
		if hostPatternMatches(normalizedHost, normalizedPort, allowedURL.Host, allowedURL.Scheme) {
			return true
		}
	}

	return hostPatternMatches(normalizedHost, normalizedPort, allowed, scheme) ||
		strings.EqualFold(strings.TrimSpace(rawHost), allowed)
}

func hostPatternMatches(normalizedHost, normalizedPort, pattern, scheme string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}

	patternHost, patternPort, hasExplicitPort := splitAllowedHostPattern(pattern, scheme)
	if patternHost == "" {
		return false
	}
	if !matchHostPattern(normalizedHost, patternHost) {
		return false
	}
	return !hasExplicitPort || normalizedPort == patternPort
}

func splitAllowedHostPattern(pattern, scheme string) (host, port string, hasExplicitPort bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", false
	}

	if parsedHost, parsedPort, err := net.SplitHostPort(pattern); err == nil {
		return normalizeHostname(parsedHost), parsedPort, true
	}

	return normalizeHostname(pattern), defaultPortForScheme(scheme), false
}

// Helper functions

// isLocalhostOrigin checks if an origin is from localhost
func isLocalhostOrigin(originURL *url.URL) bool {
	host := strings.ToLower(originURL.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// extractHeaders extracts headers from context for custom validation
func extractHeaders(c Context) map[string]string {
	headers := make(map[string]string)

	// Common headers to extract - in a real implementation,
	// you'd have access to all headers from the context
	commonHeaders := []string{
		"Origin", "User-Agent", "Accept", "Accept-Language",
		"Accept-Encoding", "Connection", "Upgrade",
		WebSocketKey, WebSocketVersion, WebSocketProtocol, WebSocketExtensions,
	}

	for _, header := range commonHeaders {
		if value := c.Header(header); value != "" {
			headers[header] = value
		}
	}

	return headers
}

// WebSocketSecurityError represents a security validation error
type WebSocketSecurityError struct {
	Code    string
	Message string
}

// Error implements the error interface
func (e *WebSocketSecurityError) Error() string {
	return fmt.Sprintf("websocket security error [%s]: %s", e.Code, e.Message)
}

// NewWebSocketSecurityError creates a new security error
func NewWebSocketSecurityError(code, message string) *WebSocketSecurityError {
	return &WebSocketSecurityError{
		Code:    code,
		Message: message,
	}
}

// SecurityMiddleware creates middleware that enforces WebSocket security policies
func SecurityMiddleware(policy WebSocketSecurityPolicy) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			// Only apply security validation to WebSocket requests
			if isWebSocketRequest(c) {
				if err := ValidateWebSocketSecurity(c, policy); err != nil {
					// Return appropriate HTTP status based on error
					if secErr, ok := err.(*WebSocketSecurityError); ok {
						switch secErr.Code {
						case "origin_required", "origin_same_origin", "origin_not_allowed":
							return c.Status(403).SendString("Forbidden: " + secErr.Message)
						case "protocol_disallowed", "protocol_not_allowed":
							return c.Status(400).SendString("Bad Request: " + secErr.Message)
						case "header_required", "header_value_mismatch":
							return c.Status(400).SendString("Bad Request: " + secErr.Message)
						default:
							return c.Status(400).SendString("Bad Request: " + secErr.Message)
						}
					}
					return c.Status(400).SendString("Bad Request: " + err.Error())
				}
			}

			return next(c)
		}
	}
}
