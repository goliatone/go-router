package router

import (
	"crypto/sha1"
	"encoding/base64"
	"net"
	"net/url"
	"strings"
)

// WebSocket GUID as defined by RFC 6455
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// isWebSocketRequest checks if the incoming request is a valid WebSocket upgrade request
func isWebSocketRequest(c Context) bool {
	// Check Connection header contains "upgrade"
	connectionHeader := strings.ToLower(c.Header("Connection"))
	if !strings.Contains(connectionHeader, "upgrade") {
		return false
	}

	// Check Upgrade header is "websocket"
	upgradeHeader := strings.ToLower(c.Header("Upgrade"))
	if upgradeHeader != "websocket" {
		return false
	}

	// Check Sec-WebSocket-Key is present and non-empty
	wsKey := c.Header(WebSocketKey)
	if wsKey == "" {
		return false
	}

	// Check Sec-WebSocket-Version is supported (typically 13)
	wsVersion := c.Header(WebSocketVersion)
	if wsVersion != "13" {
		return false
	}

	return true
}

// validateOrigin checks if the request origin is allowed based on configuration
func validateOrigin(c Context, config WebSocketConfig) bool {
	origin := strings.TrimSpace(c.Header("Origin"))

	// Use custom origin validation function if provided
	if config.CheckOrigin != nil {
		return config.CheckOrigin(origin)
	}

	// Same-origin is the safe default when no explicit allowlist is provided.
	if len(config.Origins) == 0 {
		return isSameOrigin(c)
	}

	// Check against allowed origins list
	return matchesAnyOriginPattern(origin, config.Origins)
}

// validateSubprotocols checks if requested subprotocols are supported
func validateSubprotocols(c Context, config WebSocketConfig) (string, bool) {
	// If no subprotocols are configured, any are acceptable
	if len(config.Subprotocols) == 0 {
		return "", true
	}

	// Get requested protocols from client
	requestedProtocols := c.Header(WebSocketProtocol)
	if requestedProtocols == "" {
		// Client didn't request any specific protocol
		return config.Subprotocols[0], true // Return first supported protocol
	}

	// Parse comma-separated protocols
	clientProtocols := strings.Split(requestedProtocols, ",")
	for _, clientProto := range clientProtocols {
		clientProto = strings.TrimSpace(clientProto)
		for _, serverProto := range config.Subprotocols {
			if clientProto == serverProto {
				return clientProto, true
			}
		}
	}

	return "", false
}

// generateWebSocketAccept generates the Sec-WebSocket-Accept header value
// as per RFC 6455 Section 1.3
func generateWebSocketAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// validateWebSocketKey checks if the WebSocket key is valid
func validateWebSocketKey(key string) bool {
	if key == "" {
		return false
	}

	// Decode the key to check if it's valid base64
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return false
	}

	// Key should be exactly 16 bytes when decoded
	return len(decoded) == 16
}

// isSameOrigin checks if the request is from the same origin as the server
func isSameOrigin(c Context) bool {
	origin := strings.TrimSpace(c.Header("Origin"))
	if origin == "" {
		return true // No origin header means same-origin request
	}

	host := requestHost(c)
	if host == "" {
		return false
	}

	return originMatchesRequest(origin, requestScheme(c), host)
}

func requestHost(c Context) string {
	if httpCtx, ok := c.(HTTPContext); ok {
		if req := httpCtx.Request(); req != nil && req.Host != "" {
			return strings.TrimSpace(req.Host)
		}
	}
	return strings.TrimSpace(c.Header("Host"))
}

func requestScheme(c Context) string {
	for _, header := range []string{"X-Forwarded-Proto", "X-Scheme"} {
		if value := strings.TrimSpace(c.Header(header)); value != "" {
			if idx := strings.Index(value, ","); idx >= 0 {
				value = value[:idx]
			}
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "http" || value == "https" {
				return value
			}
		}
	}

	if strings.EqualFold(strings.TrimSpace(c.Header("X-Forwarded-Ssl")), "on") {
		return "https"
	}

	if httpCtx, ok := c.(HTTPContext); ok {
		if req := httpCtx.Request(); req != nil {
			if req.TLS != nil {
				return "https"
			}
			if req.URL != nil {
				switch strings.ToLower(req.URL.Scheme) {
				case "http", "https":
					return strings.ToLower(req.URL.Scheme)
				}
			}
		}
	}

	return "http"
}

func matchesAnyOriginPattern(origin string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchesOriginPattern(origin, pattern) {
			return true
		}
	}
	return false
}

func matchesOriginPattern(origin, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "*" {
		return true
	}
	if origin == "" || pattern == "" {
		return false
	}

	originURL, err := parseOriginURL(origin)
	if err != nil {
		return false
	}

	if strings.Contains(pattern, "://") {
		patternURL, err := parseOriginURL(pattern)
		if err != nil {
			return false
		}
		if !strings.EqualFold(originURL.Scheme, patternURL.Scheme) {
			return false
		}
		if effectivePort(originURL) != effectivePort(patternURL) {
			return false
		}
		return matchHostPattern(originURL.Hostname(), patternURL.Hostname())
	}

	return matchHostPattern(originURL.Hostname(), pattern)
}

func originMatchesRequest(origin, scheme, host string) bool {
	originURL, err := parseOriginURL(origin)
	if err != nil {
		return false
	}

	requestHost, requestPort := normalizeHostAndPort(host, scheme)
	if requestHost == "" {
		return false
	}

	return strings.EqualFold(originURL.Scheme, scheme) &&
		normalizeHostname(originURL.Hostname()) == requestHost &&
		effectivePort(originURL) == requestPort
}

func parseOriginURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Hostname() == "" {
		return nil, url.InvalidHostError(raw)
	}
	return u, nil
}

func matchHostPattern(hostname, pattern string) bool {
	hostname = normalizeHostname(hostname)
	pattern = normalizeHostname(pattern)
	if hostname == "" || pattern == "" {
		return false
	}

	if strings.HasPrefix(pattern, "*.") {
		base := strings.TrimPrefix(pattern, "*.")
		return hostname != base && strings.HasSuffix(hostname, "."+base)
	}

	return hostname == pattern
}

func normalizeHostname(host string) string {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "[]")
	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}

func normalizeHostAndPort(host, scheme string) (string, string) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", ""
	}

	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		return normalizeHostname(parsedHost), parsedPort
	}

	return normalizeHostname(host), defaultPortForScheme(scheme)
}

func effectivePort(u *url.URL) string {
	if u == nil {
		return ""
	}
	if port := u.Port(); port != "" {
		return port
	}
	return defaultPortForScheme(u.Scheme)
}

func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "ws":
		return "80"
	case "https", "wss":
		return "443"
	default:
		return ""
	}
}
