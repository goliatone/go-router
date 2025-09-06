package router

import (
	"crypto/sha1"
	"encoding/base64"
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
	// If no origin validation is configured, allow all
	if config.CheckOrigin == nil && len(config.Origins) == 0 {
		return true
	}

	origin := c.Header("Origin")

	// Use custom origin validation function if provided
	if config.CheckOrigin != nil {
		return config.CheckOrigin(origin)
	}

	// Check against allowed origins list
	if len(config.Origins) > 0 {
		// Allow all origins if "*" is in the list
		for _, allowedOrigin := range config.Origins {
			if allowedOrigin == "*" {
				return true
			}
			if allowedOrigin == origin {
				return true
			}
		}
		return false
	}

	return true
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
	origin := c.Header("Origin")
	if origin == "" {
		return true // No origin header means same-origin request
	}

	// Get the host from the request
	host := c.Header("Host")
	if host == "" {
		return false
	}

	// Build expected origin
	scheme := "http"
	if c.Header("X-Forwarded-Proto") == "https" ||
		c.Header("X-Forwarded-Ssl") == "on" ||
		c.Header("X-Scheme") == "https" {
		scheme = "https"
	}

	expectedOrigin := scheme + "://" + host
	return origin == expectedOrigin
}
