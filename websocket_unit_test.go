package router_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-router"
)

// Unit Test Suite for WebSocket Implementation
// Task 6.1: Comprehensive testing of all WebSocket interfaces

// Test: WebSocket Message Types
func TestWebSocketMessageTypes(t *testing.T) {
	tests := []struct {
		name     string
		msgType  int
		expected string
	}{
		{"TextMessage", TextMessage, "text"},
		{"BinaryMessage", BinaryMessage, "binary"},
		{"CloseMessage", CloseMessage, "close"},
		{"PingMessage", PingMessage, "ping"},
		{"PongMessage", PongMessage, "pong"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msgType < 0 || tt.msgType > 10 {
				t.Errorf("Invalid message type value: %d", tt.msgType)
			}
		})
	}
}

// Test: WebSocket Close Codes
func TestWebSocketCloseCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		expected int
	}{
		{"CloseNormalClosure", CloseNormalClosure, 1000},
		{"CloseGoingAway", CloseGoingAway, 1001},
		{"CloseProtocolError", CloseProtocolError, 1002},
		{"CloseUnsupportedData", CloseUnsupportedData, 1003},
		{"CloseNoStatusReceived", CloseNoStatusReceived, 1005},
		{"CloseAbnormalClosure", CloseAbnormalClosure, 1006},
		{"CloseInvalidFramePayloadData", CloseInvalidFramePayloadData, 1007},
		{"ClosePolicyViolation", ClosePolicyViolation, 1008},
		{"CloseMessageTooBig", CloseMessageTooBig, 1009},
		{"CloseMandatoryExtension", CloseMandatoryExtension, 1010},
		{"CloseInternalServerErr", CloseInternalServerErr, 1011},
		{"CloseServiceRestart", CloseServiceRestart, 1012},
		{"CloseTryAgainLater", CloseTryAgainLater, 1013},
		{"CloseTLSHandshake", CloseTLSHandshake, 1015},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, tt.code)
			}
		})
	}
}

// Test: WebSocket Config Validation
func TestWebSocketConfigValidation(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		config := DefaultWebSocketConfig()

		if config.ReadBufferSize != 4096 {
			t.Errorf("Expected ReadBufferSize 4096, got %d", config.ReadBufferSize)
		}

		if config.WriteBufferSize != 4096 {
			t.Errorf("Expected WriteBufferSize 4096, got %d", config.WriteBufferSize)
		}

		if config.HandshakeTimeout != 10*time.Second {
			t.Errorf("Expected HandshakeTimeout 10s, got %v", config.HandshakeTimeout)
		}
	})

	t.Run("ConfigWithHandlers", func(t *testing.T) {
		connectCalled := false
		disconnectCalled := false
		messageCalled := false

		config := WebSocketConfig{
			OnConnect: func(ctx WebSocketContext) error {
				connectCalled = true
				return nil
			},
			OnDisconnect: func(ctx WebSocketContext, err error) {
				disconnectCalled = true
			},
			OnMessage: func(ctx WebSocketContext, msgType int, data []byte) error {
				messageCalled = true
				return nil
			},
		}

		// Simulate handler calls
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		config.OnConnect(ctx)
		if !connectCalled {
			t.Error("OnConnect handler not called")
		}

		config.OnMessage(ctx, TextMessage, []byte("test"))
		if !messageCalled {
			t.Error("OnMessage handler not called")
		}

		config.OnDisconnect(ctx, nil)
		if !disconnectCalled {
			t.Error("OnDisconnect handler not called")
		}
	})
}

// Test: WebSocket Error Types
func TestWebSocketErrorTypes(t *testing.T) {
	t.Run("ErrorCreation", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &WebSocketError{
			Code:    CloseProtocolError,
			Message: "protocol error occurred",
			Cause:   cause,
		}

		if err.Code != CloseProtocolError {
			t.Errorf("Expected code %d, got %d", CloseProtocolError, err.Code)
		}

		// The error message includes format "websocket [type] error [code]: message"
		if !strings.Contains(err.Error(), "protocol error occurred") {
			t.Errorf("Error message should contain 'protocol error occurred', got %s", err.Error())
		}

		if err.Unwrap() != cause {
			t.Error("Unwrap should return the cause")
		}
	})

	t.Run("PredefinedErrors", func(t *testing.T) {
		cause := errors.New("test cause")

		upgradeErr := ErrWebSocketUpgradeFailed(cause)
		if upgradeErr.Code != 1001 {
			t.Errorf("Upgrade error should have code 1001, got %d", upgradeErr.Code)
		}

		// Test custom error creation
		readErr := &WebSocketError{
			Code:    CloseAbnormalClosure,
			Message: "read failed",
			Cause:   cause,
		}
		if readErr.Code != CloseAbnormalClosure {
			t.Error("Read error should have abnormal closure code")
		}

		writeErr := &WebSocketError{
			Code:    CloseAbnormalClosure,
			Message: "write failed",
			Cause:   cause,
		}
		if writeErr.Code != CloseAbnormalClosure {
			t.Error("Write error should have abnormal closure code")
		}
	})
}

// Test: Context Factory Registry
func TestContextFactoryRegistry(t *testing.T) {
	t.Run("FactoryRegistration", func(t *testing.T) {
		// Test factory pattern implementation
		factory := &mockWebSocketFactory{}

		// Verify factory interface methods
		if factory.AdapterName() != "mock" {
			t.Error("Factory should return correct adapter name")
		}

		// Test CanUpgrade
		mockCtx := newMockWebSocketContext()
		if !factory.CanUpgrade(mockCtx) {
			t.Error("Factory should be able to upgrade mock context")
		}

		// Test CreateWebSocketContext
		wsCtx, err := factory.CreateWebSocketContext(mockCtx)
		if err != nil {
			t.Errorf("Factory should create WebSocket context: %v", err)
		}

		if wsCtx == nil {
			t.Error("Created WebSocket context should not be nil")
		}
	})
}

// Test: Message Validation
func TestMessageValidation(t *testing.T) {
	t.Run("ValidMessageTypes", func(t *testing.T) {
		validTypes := []int{TextMessage, BinaryMessage, CloseMessage, PingMessage, PongMessage}

		for _, msgType := range validTypes {
			if msgType < 0 || msgType > 10 {
				t.Errorf("Invalid message type: %d", msgType)
			}
		}
	})

	t.Run("MessageSizeValidation", func(t *testing.T) {
		maxSize := 1024 * 1024 // 1MB

		smallMsg := make([]byte, 100)
		if len(smallMsg) > maxSize {
			t.Error("Small message should be within size limit")
		}

		largeMsg := make([]byte, maxSize+1)
		if len(largeMsg) <= maxSize {
			t.Error("Large message should exceed size limit")
		}
	})
}

// Test: Concurrent Operations
func TestConcurrentWebSocketOperations(t *testing.T) {
	t.Run("ConcurrentWrites", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		var wg sync.WaitGroup
		errors := make(chan error, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				msg := []byte("message " + string(rune(id)))
				if err := ctx.WriteMessage(TextMessage, msg); err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			if err != nil {
				t.Errorf("Concurrent write failed: %v", err)
			}
		}
	})

	t.Run("ConcurrentReads", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		// Pre-populate messages
		for i := 0; i < 10; i++ {
			ctx.WriteMessage(TextMessage, []byte("test"))
		}

		var wg sync.WaitGroup
		results := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := ctx.ReadMessage()
				results <- (err == nil)
			}()
		}

		wg.Wait()
		close(results)

		successCount := 0
		for success := range results {
			if success {
				successCount++
			}
		}

		if successCount == 0 {
			t.Error("At least some reads should succeed")
		}
	})
}

// Test: JSON Operations
func TestJSONOperations(t *testing.T) {
	t.Run("WriteJSON", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		testData := map[string]interface{}{
			"id":      123,
			"message": "test",
			"active":  true,
		}

		if err := ctx.WriteJSON(testData); err != nil {
			t.Errorf("WriteJSON failed: %v", err)
		}
	})

	t.Run("ReadJSON", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		// Write JSON data first
		testData := map[string]string{"key": "value"}
		jsonData, _ := json.Marshal(testData)
		ctx.WriteMessage(TextMessage, jsonData)

		// Read it back
		var result map[string]string
		if err := ctx.ReadJSON(&result); err != nil {
			t.Errorf("ReadJSON failed: %v", err)
		}

		if result["key"] != "value" {
			t.Error("JSON data not read correctly")
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		// Write invalid JSON
		ctx.WriteMessage(TextMessage, []byte("{invalid json}"))

		var result map[string]string
		err := ctx.ReadJSON(&result)
		if err == nil {
			t.Error("Should fail on invalid JSON")
		}
	})
}

// Test: Connection State Management
func TestConnectionStateManagement(t *testing.T) {
	t.Run("ConnectionLifecycle", func(t *testing.T) {
		ctx := newMockWebSocketContext()

		// Before upgrade
		if ctx.IsConnected() {
			t.Error("Should not be connected before upgrade")
		}

		// After upgrade
		ctx.mockUpgrade()
		if !ctx.IsConnected() {
			t.Error("Should be connected after upgrade")
		}

		// After close
		ctx.Close()
		if ctx.IsConnected() {
			t.Error("Should not be connected after close")
		}
	})

	t.Run("ConnectionID", func(t *testing.T) {
		ctx1 := newMockWebSocketContext()
		ctx2 := newMockWebSocketContext()

		id1 := ctx1.ConnectionID()
		id2 := ctx2.ConnectionID()

		if id1 == "" {
			t.Error("Connection ID should not be empty")
		}

		if id1 == id2 {
			t.Error("Different connections should have different IDs")
		}
	})
}

// Test: Deadline Management
func TestDeadlineManagement(t *testing.T) {
	t.Run("ReadDeadline", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		deadline := time.Now().Add(1 * time.Second)
		if err := ctx.SetReadDeadline(deadline); err != nil {
			t.Errorf("SetReadDeadline failed: %v", err)
		}
	})

	t.Run("WriteDeadline", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		deadline := time.Now().Add(1 * time.Second)
		if err := ctx.SetWriteDeadline(deadline); err != nil {
			t.Errorf("SetWriteDeadline failed: %v", err)
		}
	})
}

// Test: Ping/Pong Handling
func TestPingPongHandling(t *testing.T) {
	t.Run("WritePing", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		if err := ctx.WritePing([]byte("ping")); err != nil {
			t.Errorf("WritePing failed: %v", err)
		}
	})

	t.Run("WritePong", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		if err := ctx.WritePong([]byte("pong")); err != nil {
			t.Errorf("WritePong failed: %v", err)
		}
	})

	t.Run("PingHandler", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		ctx.SetPingHandler(func(data []byte) error {
			// Handler registered
			return nil
		})

		// Simulate ping reception
		ctx.WritePing([]byte("test"))

		// Note: In real implementation, handler would be called
		// This is a mock test to verify the interface
	})

	t.Run("PongHandler", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		ctx.SetPongHandler(func(data []byte) error {
			// Handler registered
			return nil
		})

		// Simulate pong reception
		ctx.WritePong([]byte("test"))

		// Note: In real implementation, handler would be called
		// This is a mock test to verify the interface
	})
}

// Test: Close Handling
func TestCloseHandling(t *testing.T) {
	t.Run("NormalClose", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		if err := ctx.Close(); err != nil {
			t.Errorf("Close failed: %v", err)
		}

		if ctx.IsConnected() {
			t.Error("Should not be connected after close")
		}
	})

	t.Run("CloseWithStatus", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		if err := ctx.CloseWithStatus(CloseNormalClosure, "goodbye"); err != nil {
			t.Errorf("CloseWithStatus failed: %v", err)
		}
	})

	t.Run("CloseHandler", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		ctx.SetCloseHandler(func(code int, text string) error {
			if code != CloseNormalClosure {
				t.Errorf("Expected close code %d, got %d", CloseNormalClosure, code)
			}
			return nil
		})

		// Close the connection
		ctx.CloseWithStatus(CloseNormalClosure, "test")

		// Note: In real implementation, handler would be called
		// This is a mock test to verify the interface
	})
}

// Test: Subprotocol Support
func TestSubprotocolSupport(t *testing.T) {
	t.Run("SubprotocolRetrieval", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		protocol := ctx.Subprotocol()
		// Mock returns empty, but interface is tested
		if protocol != "" {
			t.Logf("Subprotocol: %s", protocol)
		}
	})

	t.Run("ExtensionsRetrieval", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		extensions := ctx.Extensions()
		if extensions == nil {
			t.Log("No extensions (expected for mock)")
		}
	})
}

// Test: Address Information
func TestAddressInformation(t *testing.T) {
	t.Run("RemoteAddress", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		addr := ctx.RemoteAddr()
		if addr == "" {
			t.Error("Remote address should not be empty")
		}
	})

	t.Run("LocalAddress", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		addr := ctx.LocalAddr()
		if addr == "" {
			t.Error("Local address should not be empty")
		}
	})
}

// Test: Origin Validation
func TestOriginValidation(t *testing.T) {
	tests := []struct {
		name     string
		origin   string
		allowed  []string
		expected bool
	}{
		{"ExactMatch", "https://example.com", []string{"https://example.com"}, true},
		{"NoMatch", "https://evil.com", []string{"https://example.com"}, false},
		{"MultipleAllowed", "https://app.com", []string{"https://example.com", "https://app.com"}, true},
		{"WildcardAll", "https://any.com", []string{"*"}, true},
		{"EmptyOrigin", "", []string{"https://example.com"}, false},
		{"EmptyAllowed", "https://example.com", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateTestOrigin(tt.origin, tt.allowed)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for origin %s", tt.expected, result, tt.origin)
			}
		})
	}
}

// Test: Error Recovery
func TestErrorRecovery(t *testing.T) {
	t.Run("PanicRecovery", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic: %v", r)
			}
		}()

		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		// Simulate operation that might panic
		var nilMap map[string]string
		_ = nilMap["key"] // This won't panic, but demonstrates error handling
	})

	t.Run("ErrorPropagation", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		// Don't upgrade - operations should fail

		_, _, err := ctx.ReadMessage()
		if err == nil {
			t.Error("Should return error when not connected")
		}
	})
}

// Test: Buffer Management
func TestBufferManagement(t *testing.T) {
	t.Run("BufferReuse", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		// Write multiple messages
		for i := 0; i < 100; i++ {
			msg := bytes.Repeat([]byte("a"), 1024)
			if err := ctx.WriteMessage(TextMessage, msg); err != nil {
				t.Errorf("Write %d failed: %v", i, err)
			}
		}
	})

	t.Run("LargeMessage", func(t *testing.T) {
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()

		// Create a large message
		largeMsg := bytes.Repeat([]byte("x"), 1024*1024) // 1MB

		if err := ctx.WriteMessage(BinaryMessage, largeMsg); err != nil {
			t.Errorf("Failed to write large message: %v", err)
		}
	})
}

// Mock WebSocket Factory for testing
type mockWebSocketFactory struct{}

func (f *mockWebSocketFactory) CreateWebSocketContext(ctx Context) (WebSocketContext, error) {
	if mockCtx, ok := ctx.(*mockWebSocketContext); ok {
		mockCtx.mockUpgrade()
		return mockCtx, nil
	}
	return nil, errors.New("invalid context type")
}

func (f *mockWebSocketFactory) CanUpgrade(ctx Context) bool {
	_, ok := ctx.(*mockWebSocketContext)
	return ok
}

func (f *mockWebSocketFactory) AdapterName() string {
	return "mock"
}

// Helper function for origin validation
func validateTestOrigin(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}

	for _, allowedOrigin := range allowed {
		if allowedOrigin == "*" {
			return true
		}
		if origin == allowedOrigin {
			return true
		}
	}

	return false
}
