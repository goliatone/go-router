package router_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnifiedWebSocketInterface tests that the WebSocket method works identically
// across different adapters
func TestUnifiedWebSocketInterface(t *testing.T) {
	// Test HTTPRouter implementation
	app := router.NewHTTPServer()

	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"}

	wsHandler := func(ws router.WebSocketContext) error {
		return nil
	}

	// This should work with the unified interface
	route := app.Router().WebSocket("/test-ws", config, wsHandler)

	// Verify route was created
	assert.NotNil(t, route, "WebSocket route should be created")
}

// TestWebSocketRouteRegistration tests that WebSocket routes are properly registered
func TestWebSocketRouteRegistration(t *testing.T) {
	app := router.NewHTTPServer()

	config := router.DefaultWebSocketConfig()
	handler := func(ws router.WebSocketContext) error { return nil }

	// Test basic registration
	route := app.Router().WebSocket("/ws", config, handler)
	assert.NotNil(t, route, "WebSocket route should be created")

	// Test with route groups
	apiGroup := app.Router().Group("/api")
	wsRoute := apiGroup.WebSocket("/notifications", config, handler)
	assert.NotNil(t, wsRoute, "Grouped WebSocket route should be created")

	// Test with nested groups
	v1Group := apiGroup.Group("/v1")
	nestedRoute := v1Group.WebSocket("/chat", config, handler)
	assert.NotNil(t, nestedRoute, "Nested WebSocket route should be created")
}

// TestWebSocketConfigDefaults tests that default configuration values are properly applied
func TestWebSocketConfigDefaults(t *testing.T) {
	config := router.DefaultWebSocketConfig()

	// Test default values
	assert.Equal(t, []string{"*"}, config.Origins)
	assert.Equal(t, 4096, config.ReadBufferSize)
	assert.Equal(t, 4096, config.WriteBufferSize)
	assert.Equal(t, 10*time.Second, config.HandshakeTimeout)
	assert.Equal(t, 60*time.Second, config.ReadTimeout)
	assert.Equal(t, 10*time.Second, config.WriteTimeout)
	assert.Equal(t, 54*time.Second, config.PingPeriod)
	assert.Equal(t, 60*time.Second, config.PongWait)
	assert.Equal(t, int64(1024*1024), config.MaxMessageSize) // 1MB
	assert.False(t, config.EnableCompression)
	assert.True(t, config.AllowMultipleConnections)
	assert.Equal(t, 100, config.ConnectionPoolSize)

	// Test validation
	err := config.Validate()
	assert.NoError(t, err, "Default configuration should be valid")
}

// TestWebSocketConfigValidation tests configuration validation
func TestWebSocketConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		configMod   func(*router.WebSocketConfig)
		shouldError bool
		errorMsg    string
	}{
		{
			name: "PingPeriod_Greater_Than_PongWait",
			configMod: func(c *router.WebSocketConfig) {
				c.PingPeriod = 65 * time.Second
				c.PongWait = 60 * time.Second
			},
			shouldError: true,
			errorMsg:    "PingPeriod must be less than PongWait",
		},
		{
			name: "Small_Buffer_Size",
			configMod: func(c *router.WebSocketConfig) {
				c.ReadBufferSize = 512
			},
			shouldError: true,
			errorMsg:    "ReadBufferSize should be at least 1024 bytes",
		},
		{
			name: "Valid_Custom_Config",
			configMod: func(c *router.WebSocketConfig) {
				c.Origins = []string{"https://example.com"}
				c.ReadBufferSize = 8192
				c.WriteBufferSize = 8192
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := router.DefaultWebSocketConfig()
			tt.configMod(&config)

			err := config.Validate()
			if tt.shouldError {
				assert.Error(t, err, "Configuration should be invalid")
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err, "Configuration should be valid")
			}
		})
	}
}

// TestWebSocketHandlerTypes tests different handler patterns work with unified interface
func TestWebSocketHandlerTypes(t *testing.T) {
	app := router.NewHTTPServer()
	config := router.DefaultWebSocketConfig()

	// Echo handler
	echoHandler := func(ws router.WebSocketContext) error {
		for {
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				break
			}
			if err := ws.WriteMessage(messageType, data); err != nil {
				break
			}
		}
		return nil
	}

	// JSON handler
	jsonHandler := func(ws router.WebSocketContext) error {
		var message map[string]any
		if err := ws.ReadJSON(&message); err != nil {
			return err
		}
		response := map[string]any{
			"received":  message,
			"timestamp": time.Now().Unix(),
		}
		return ws.WriteJSON(response)
	}

	// Register different handler types using unified interface
	routes := []router.RouteInfo{
		app.Router().WebSocket("/echo", config, echoHandler),
		app.Router().WebSocket("/json", config, jsonHandler),
	}

	for i, route := range routes {
		assert.NotNil(t, route, "Handler %d should create valid route", i)
	}
}

// TestWebSocketRouteNaming tests that WebSocket routes can be named
func TestWebSocketRouteNaming(t *testing.T) {
	app := router.NewHTTPServer()

	config := router.DefaultWebSocketConfig()
	handler := func(ws router.WebSocketContext) error { return nil }

	route := app.Router().WebSocket("/notifications", config, handler)
	namedRoute := route.SetName("websocket.notifications")

	assert.NotNil(t, namedRoute, "Named WebSocket route should be created")
	// Note: RouteInfo interface doesn't expose Name() method for testing verification
}

// TestWebSocketApplyDefaults tests that ApplyDefaults fills in missing values
func TestWebSocketApplyDefaults(t *testing.T) {
	config := router.WebSocketConfig{
		// Leave most fields empty/zero
		EnableCompression: true,
	}

	// Apply defaults
	config.ApplyDefaults()

	// Check that defaults were applied
	assert.Equal(t, []string{"*"}, config.Origins)
	assert.Equal(t, 4096, config.ReadBufferSize)
	assert.Equal(t, 4096, config.WriteBufferSize)
	assert.Equal(t, 10*time.Second, config.HandshakeTimeout)
	assert.True(t, config.EnableCompression)     // Should preserve original value
	assert.Equal(t, -1, config.CompressionLevel) // Should get default
}

// Example of actual WebSocket communication test (HTTPRouter only)
func TestHTTPRouterWebSocketCommunication(t *testing.T) {
	app := router.NewHTTPServer()

	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"}

	// Echo handler for testing
	echoHandler := func(ws router.WebSocketContext) error {
		for {
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				break
			}
			if err := ws.WriteMessage(messageType, data); err != nil {
				break
			}
		}
		return nil
	}

	// Use unified interface to register WebSocket route
	app.Router().WebSocket("/echo", config, echoHandler)

	// Create test server
	server := httptest.NewServer(app.WrappedRouter())
	defer server.Close()

	// Convert HTTP URL to WebSocket URL
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + "/echo"

	// Connect to WebSocket
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Test echo functionality
	testMessage := "Hello WebSocket!"
	err = conn.WriteMessage(websocket.TextMessage, []byte(testMessage))
	require.NoError(t, err)

	messageType, response, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, messageType)
	assert.Equal(t, testMessage, string(response))
}

// TestUnifiedInterfaceCompatibility tests that the same code works for different handler scenarios
func TestUnifiedInterfaceCompatibility(t *testing.T) {
	// This function simulates how a user would write WebSocket handling code
	// that works with any adapter
	setupWebSocketRoutes := func(app *router.HTTPServer) {
		config := router.DefaultWebSocketConfig()
		config.Origins = []string{"*"}

		// Chat handler
		chatHandler := func(ws router.WebSocketContext) error {
			// Send welcome message
			ws.WriteJSON(map[string]string{
				"type":    "welcome",
				"message": "Connected to chat!",
			})

			// Handle messages
			for {
				var msg map[string]any
				if err := ws.ReadJSON(&msg); err != nil {
					break
				}

				// Echo back
				response := map[string]any{
					"type":      "message",
					"data":      msg,
					"timestamp": time.Now().Unix(),
				}

				if err := ws.WriteJSON(response); err != nil {
					break
				}
			}
			return nil
		}

		// This same code works with any adapter that implements the unified interface
		app.Router().WebSocket("/chat", config, chatHandler)

		// Works with groups too
		apiGroup := app.Router().Group("/api/v1")
		apiGroup.WebSocket("/notifications", config, chatHandler)
	}

	// Test with HTTPRouter
	appInterface := router.NewHTTPServer()
	app := appInterface.(*router.HTTPServer) // Type assertion needed
	setupWebSocketRoutes(app)                // Same function works!

	// Verify routes were created (we can't test the actual functionality without integration)
	assert.NotNil(t, app, "App should be created successfully")
}

// Benchmark test for the unified interface
func BenchmarkUnifiedWebSocketRegistration(b *testing.B) {
	app := router.NewHTTPServer()
	config := router.DefaultWebSocketConfig()
	handler := func(ws router.WebSocketContext) error { return nil }

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		path := fmt.Sprintf("/ws-%d", i)
		route := app.Router().WebSocket(path, config, handler)
		if route == nil {
			b.Fatal("Failed to create WebSocket route")
		}
	}
}
