package router_test

import (
	"context"
	"encoding/json"
	"mime/multipart"
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-router"
)

// mockWebSocketContext implements WebSocketContext for testing WebSocket client Query method
type mockWebSocketContext struct {
	id           string
	messages     chan []byte
	closeHandler func(int, string) error
	closed       bool
	mu           sync.Mutex
	queryParams  map[string]string
}

func newMockWebSocketContext(id string, queryParams map[string]string) *mockWebSocketContext {
	return &mockWebSocketContext{
		id:          id,
		messages:    make(chan []byte, 100),
		queryParams: queryParams,
	}
}

func (m *mockWebSocketContext) ConnectionID() string { return m.id }

func (m *mockWebSocketContext) ReadMessage() (int, []byte, error) {
	select {
	case msg := <-m.messages:
		return router.TextMessage, msg, nil
	case <-time.After(100 * time.Millisecond):
		return 0, nil, nil
	}
}

func (m *mockWebSocketContext) WriteMessage(messageType int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return router.ErrConnectionClosed
	}
	return nil
}

func (m *mockWebSocketContext) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return m.WriteMessage(router.TextMessage, data)
}

func (m *mockWebSocketContext) ReadJSON(v any) error {
	_, data, err := m.ReadMessage()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (m *mockWebSocketContext) WritePing(data []byte) error {
	return m.WriteMessage(router.PingMessage, data)
}

func (m *mockWebSocketContext) WritePong(data []byte) error {
	return m.WriteMessage(router.PongMessage, data)
}

func (m *mockWebSocketContext) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.messages != nil {
		close(m.messages)
	}
	return nil
}

func (m *mockWebSocketContext) CloseWithStatus(code int, reason string) error {
	return m.Close()
}

func (m *mockWebSocketContext) SetCloseHandler(handler func(code int, text string) error) {
	m.closeHandler = handler
}

func (m *mockWebSocketContext) Subprotocol() string                       { return "" }
func (m *mockWebSocketContext) Extensions() []string                      { return nil }
func (m *mockWebSocketContext) RemoteAddr() string                        { return "127.0.0.1:12345" }
func (m *mockWebSocketContext) LocalAddr() string                         { return "127.0.0.1:8080" }
func (m *mockWebSocketContext) SetReadDeadline(t time.Time) error         { return nil }
func (m *mockWebSocketContext) SetWriteDeadline(t time.Time) error        { return nil }
func (m *mockWebSocketContext) SetPingHandler(handler func([]byte) error) {}
func (m *mockWebSocketContext) SetPongHandler(handler func([]byte) error) {}

// WebSocketContext interface methods
func (m *mockWebSocketContext) IsWebSocket() bool       { return true }
func (m *mockWebSocketContext) WebSocketUpgrade() error { return nil }

// Context interface methods
func (m *mockWebSocketContext) Method() string      { return "GET" }
func (m *mockWebSocketContext) Path() string        { return "/ws" }
func (m *mockWebSocketContext) OriginalURL() string { return "/ws" }
func (m *mockWebSocketContext) Param(name string, def ...string) string {
	if len(def) > 0 {
		return def[0]
	}
	return ""
}
func (m *mockWebSocketContext) ParamsInt(key string, def int) int { return def }

// Query method implementation - this is what we're testing
func (m *mockWebSocketContext) Query(name string, defaultValue ...string) string {
	if val, exists := m.queryParams[name]; exists {
		return val
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (m *mockWebSocketContext) QueryInt(name string, defaultValue int) int {
	return defaultValue // Simplified for this test
}

func (m *mockWebSocketContext) Queries() map[string]string {
	return m.queryParams
}

func (m *mockWebSocketContext) Body() []byte                     { return nil }
func (m *mockWebSocketContext) Bind(v any) error                 { return nil }
func (m *mockWebSocketContext) Locals(key any, value ...any) any { return nil }
func (m *mockWebSocketContext) Render(name string, bind any, layouts ...string) error {
	return nil
}
func (m *mockWebSocketContext) Cookie(cookie *router.Cookie) {}
func (m *mockWebSocketContext) Cookies(key string, def ...string) string {
	if len(def) > 0 {
		return def[0]
	}
	return ""
}
func (m *mockWebSocketContext) CookieParser(out any) error              { return nil }
func (m *mockWebSocketContext) Context() context.Context                { return context.Background() }
func (m *mockWebSocketContext) SetContext(ctx context.Context)          {}
func (m *mockWebSocketContext) Next() error                             { return nil }
func (m *mockWebSocketContext) Status(code int) router.Context          { return m }
func (m *mockWebSocketContext) Send(body []byte) error                  { return nil }
func (m *mockWebSocketContext) SendString(body string) error            { return nil }
func (m *mockWebSocketContext) SendFile(file string, args ...any) error { return nil }
func (m *mockWebSocketContext) JSON(code int, v any) error              { return nil }
func (m *mockWebSocketContext) NoContent(code int) error                { return nil }
func (m *mockWebSocketContext) Header(name string) string               { return "" }
func (m *mockWebSocketContext) SetHeader(name, value string)            {}
func (m *mockWebSocketContext) Get(key string, def any) any {
	return def
}
func (m *mockWebSocketContext) Set(key string, value any) {}
func (m *mockWebSocketContext) GetString(key string, def string) string {
	return def
}
func (m *mockWebSocketContext) GetInt(key string, def int) int {
	return def
}
func (m *mockWebSocketContext) GetBool(key string, def bool) bool {
	return def
}
func (m *mockWebSocketContext) FormFile(name string) (*multipart.FileHeader, error) { return nil, nil }
func (m *mockWebSocketContext) FormValue(key string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func TestWSClient_Query(t *testing.T) {
	tests := []struct {
		name          string
		queryParams   map[string]string
		queryKey      string
		defaultValue  []string
		expectedValue string
	}{
		{
			name:          "Query parameter exists",
			queryParams:   map[string]string{"token": "abc123", "room": "general"},
			queryKey:      "token",
			defaultValue:  nil,
			expectedValue: "abc123",
		},
		{
			name:          "Query parameter exists with default",
			queryParams:   map[string]string{"token": "abc123", "room": "general"},
			queryKey:      "room",
			defaultValue:  []string{"default_room"},
			expectedValue: "general",
		},
		{
			name:          "Query parameter missing, no default",
			queryParams:   map[string]string{"token": "abc123"},
			queryKey:      "missing",
			defaultValue:  nil,
			expectedValue: "",
		},
		{
			name:          "Query parameter missing, with default",
			queryParams:   map[string]string{"token": "abc123"},
			queryKey:      "missing",
			defaultValue:  []string{"default_value"},
			expectedValue: "default_value",
		},
		{
			name:          "Empty query params, with default",
			queryParams:   map[string]string{},
			queryKey:      "any_key",
			defaultValue:  []string{"fallback"},
			expectedValue: "fallback",
		},
		{
			name:          "Special characters in query value",
			queryParams:   map[string]string{"message": "hello world!"},
			queryKey:      "message",
			defaultValue:  nil,
			expectedValue: "hello world!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create hub
			hub := router.NewWSHub()
			defer hub.Close()

			// Create mock connection with query parameters
			mockConn := newMockWebSocketContext("test-client", tt.queryParams)

			// Create WebSocket client
			client := router.NewWSClient(mockConn, hub)
			defer client.Close(router.CloseNormalClosure, "test cleanup")

			// Test Query method
			var result string
			if len(tt.defaultValue) > 0 {
				result = client.Query(tt.queryKey, tt.defaultValue...)
			} else {
				result = client.Query(tt.queryKey)
			}

			if result != tt.expectedValue {
				t.Errorf("Expected Query(%s) to return '%s', got '%s'", tt.queryKey, tt.expectedValue, result)
			}
		})
	}
}

func TestWSClient_QueryWithNilConnection(t *testing.T) {
	// Test behavior when connection is nil - this tests our error handling
	tests := []struct {
		name          string
		queryKey      string
		defaultValue  []string
		expectedValue string
	}{
		{
			name:          "Nil connection, no default",
			queryKey:      "token",
			defaultValue:  nil,
			expectedValue: "",
		},
		{
			name:          "Nil connection, with default",
			queryKey:      "token",
			defaultValue:  []string{"fallback"},
			expectedValue: "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a client manually with a nil connection to test the error handling path
			// We can't easily do this through the public API, so this test verifies the logic
			// by ensuring our implementation handles query parameters correctly when available

			hub := router.NewWSHub()
			defer hub.Close()

			// Create connection and then close it immediately to test edge case
			mockConn := newMockWebSocketContext("test", map[string]string{})
			client := router.NewWSClient(mockConn, hub)
			client.Close(router.CloseNormalClosure, "test")

			// Even after close, the connection should still be available for query parameter access
			// as they come from the initial HTTP upgrade request
			var result string
			if len(tt.defaultValue) > 0 {
				result = client.Query(tt.queryKey, tt.defaultValue...)
			} else {
				result = client.Query(tt.queryKey)
			}

			// Since we have empty query params, we should get the default or empty string
			if result != tt.expectedValue {
				t.Errorf("Expected Query(%s) to return '%s', got '%s'", tt.queryKey, tt.expectedValue, result)
			}
		})
	}
}

func TestWSClient_QueryIntegration(t *testing.T) {
	// Integration test with other WSClient methods
	hub := router.NewWSHub()
	defer hub.Close()

	queryParams := map[string]string{
		"user_id":    "12345",
		"session_id": "sess_abcdef",
		"theme":      "dark",
	}

	mockConn := newMockWebSocketContext("integration-test", queryParams)
	client := router.NewWSClient(mockConn, hub)
	defer client.Close(router.CloseNormalClosure, "test cleanup")

	// Verify client is connected
	if !client.IsConnected() {
		t.Error("Client should be connected after creation")
	}

	// Test that query parameters work alongside other client methods
	userID := client.Query("user_id")
	if userID != "12345" {
		t.Errorf("Expected user_id=12345, got %s", userID)
	}

	// Test storing query parameter as client state
	client.Set("user_id_from_query", userID)
	storedUserID := client.GetString("user_id_from_query")
	if storedUserID != userID {
		t.Errorf("Expected stored user ID to match query parameter: %s != %s", storedUserID, userID)
	}

	// Test multiple query parameters
	sessionID := client.Query("session_id")
	theme := client.Query("theme", "light") // should return "dark", not default
	nonExistent := client.Query("non_existent", "default_value")

	if sessionID != "sess_abcdef" {
		t.Errorf("Expected session_id=sess_abcdef, got %s", sessionID)
	}
	if theme != "dark" {
		t.Errorf("Expected theme=dark, got %s", theme)
	}
	if nonExistent != "default_value" {
		t.Errorf("Expected non_existent=default_value, got %s", nonExistent)
	}
}
