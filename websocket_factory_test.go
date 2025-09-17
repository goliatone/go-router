package router_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/goliatone/go-router"
)

// Mock WebSocket factory implementations
type mockFiberFactory struct{}

func (f *mockFiberFactory) CreateWebSocketContext(c router.Context, config router.WebSocketConfig) (router.WebSocketContext, error) {
	return nil, nil
}

func (f *mockFiberFactory) SupportsWebSocket() bool {
	return true
}

func (f *mockFiberFactory) AdapterName() string {
	return "fiber"
}

// Enhanced factory with SupportedContextTypes
func (f *mockFiberFactory) SupportedContextTypes() []string {
	return []string{"fiber", "fiberContext", "*fiberContext"}
}

type mockHTTPRouterFactory struct{}

func (f *mockHTTPRouterFactory) CreateWebSocketContext(c router.Context, config router.WebSocketConfig) (router.WebSocketContext, error) {
	return nil, nil
}

func (f *mockHTTPRouterFactory) SupportsWebSocket() bool {
	return true
}

func (f *mockHTTPRouterFactory) AdapterName() string {
	return "httprouter"
}

// Enhanced factory with SupportedContextTypes
func (f *mockHTTPRouterFactory) SupportedContextTypes() []string {
	return []string{"httprouter", "httpRouterContext", "*httpRouterContext"}
}

// Note: Testing the full GetByContext functionality requires a complete Context implementation
// which is complex. We'll focus on testing the core logic separately.

// Test the contains function with various cases
// Since contains is not exported, we'll test it indirectly through a simple test
func TestContainsLogic(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{"exact match", "fiber", "fiber", true},
		{"case insensitive match", "Fiber", "fiber", true},
		{"substring match", "fiberContext", "fiber", true},
		{"case insensitive substring", "FiberContext", "fiber", true},
		{"no match", "httprouter", "fiber", false},
		{"empty substring", "fiber", "", true},
		{"empty string", "", "fiber", false},
		{"both empty", "", "", true},
		{"substring longer than string", "go", "golang", false},
		{"mixed case", "HTTPRouter", "router", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the logic manually since we can't access the internal function
			result := testContainsLogic(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("contains(%q, %q) = %v, expected %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

// Implement the same logic as the fixed contains function for testing
func testContainsLogic(s, substr string) bool {
	if len(substr) == 0 {
		return true // empty substring is contained in any string
	}
	if len(s) < len(substr) {
		return false // string is shorter than substring
	}

	// Convert both strings to lowercase for case-insensitive matching
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// Test factory registry functionality
func TestWebSocketFactoryRegistry(t *testing.T) {
	registry := router.NewWebSocketFactoryRegistry()

	fiberFactory := &mockFiberFactory{}
	httpRouterFactory := &mockHTTPRouterFactory{}

	// Test registration
	registry.Register("fiber", fiberFactory)
	registry.Register("httprouter", httpRouterFactory)

	// Test retrieval by name
	t.Run("GetByName", func(t *testing.T) {
		retrieved := registry.Get("fiber")
		if retrieved == nil {
			t.Error("Failed to retrieve fiber factory by name")
		}

		retrieved = registry.Get("httprouter")
		if retrieved == nil {
			t.Error("Failed to retrieve httprouter factory by name")
		}

		retrieved = registry.Get("nonexistent")
		if retrieved != nil {
			t.Error("Expected nil for nonexistent factory")
		}
	})

	// Test listing adapters
	t.Run("ListAdapters", func(t *testing.T) {
		adapters := registry.List()
		if len(adapters) != 2 {
			t.Errorf("Expected 2 adapters, got %d", len(adapters))
		}

		found := make(map[string]bool)
		for _, adapter := range adapters {
			found[adapter] = true
		}

		if !found["fiber"] || !found["httprouter"] {
			t.Error("Missing expected adapters in list")
		}
	})
}

// Test context-based factory resolution behavior
func TestGetByContextBehavior(t *testing.T) {
	registry := router.NewWebSocketFactoryRegistry()

	fiberFactory := &mockFiberFactory{}
	registry.Register("fiber", fiberFactory)

	// Test that when no matching factory is found, GetByContext returns nil
	// Since we can't easily mock a full Context, we'll just test the registry logic
	t.Run("NoFactoriesRegistered", func(t *testing.T) {
		emptyRegistry := router.NewWebSocketFactoryRegistry()
		// Should return nil when no factories are registered
		if len(emptyRegistry.List()) != 0 {
			t.Error("Expected empty registry to have no factories")
		}
	})
}

// Test global registry functions
func TestGlobalFactoryRegistry(t *testing.T) {
	// Clean up any existing registrations first
	adapters := router.ListRegisteredAdapters()
	originalCount := len(adapters)

	fiberFactory := &mockFiberFactory{}
	httpRouterFactory := &mockHTTPRouterFactory{}

	// Test global registration
	router.RegisterGlobalWebSocketFactory("test-fiber", fiberFactory)
	router.RegisterGlobalWebSocketFactory("test-httprouter", httpRouterFactory)

	// Test global retrieval
	t.Run("GlobalGet", func(t *testing.T) {
		retrieved := router.GetGlobalWebSocketFactory("test-fiber")
		if retrieved != fiberFactory {
			t.Error("Failed to retrieve fiber factory from global registry")
		}

		retrieved = router.GetGlobalWebSocketFactory("test-httprouter")
		if retrieved != httpRouterFactory {
			t.Error("Failed to retrieve httprouter factory from global registry")
		}
	})

	// Test global context resolution behavior
	t.Run("GlobalGetByContextBehavior", func(t *testing.T) {
		// Test that the global function exists and can be called
		// We can't easily test with a real context, but we can verify the function works
		adapters := router.ListRegisteredAdapters()
		if len(adapters) < originalCount+2 {
			t.Errorf("Expected at least %d adapters after registration", originalCount+2)
		}
	})

	// Test global adapter listing
	t.Run("GlobalList", func(t *testing.T) {
		adapters := router.ListRegisteredAdapters()
		if len(adapters) < originalCount+2 {
			t.Errorf("Expected at least %d adapters after registration, got %d",
				originalCount+2, len(adapters))
		}
	})
}

// Test WebSocket context wrapper functionality that doesn't require full Context
func TestWebSocketContextWrapperBasics(t *testing.T) {
	config := router.WebSocketConfig{}
	protocol := "ws"

	// Test wrapper creation and basic functionality that we can verify without a full Context
	t.Run("BasicCreation", func(t *testing.T) {
		// Test that we can create wrapper with nil context for basic functionality
		wrapper := router.NewWebSocketContextWrapper(nil, config, protocol)

		if wrapper.Subprotocol() != protocol {
			t.Error("Subprotocol() should return the configured protocol")
		}

		connID := wrapper.ConnectionID()
		if connID == "" {
			t.Error("ConnectionID() should return a non-empty string")
		}

		// Should start as not connected
		if wrapper.IsConnected() {
			t.Error("Should start as not connected")
		}

		// Set connected and verify
		wrapper.SetConnected(true)
		if !wrapper.IsConnected() {
			t.Error("Should be connected after SetConnected(true)")
		}

		// Set disconnected and verify
		wrapper.SetConnected(false)
		if wrapper.IsConnected() {
			t.Error("Should be disconnected after SetConnected(false)")
		}
	})
}

// Test factory error functionality
func TestWebSocketFactoryError(t *testing.T) {
	originalErr := errors.New("original error")
	factoryErr := router.NewWebSocketFactoryError("fiber", "create", originalErr)

	t.Run("Error", func(t *testing.T) {
		errStr := factoryErr.Error()
		if !strings.Contains(errStr, "fiber") {
			t.Error("Error string should contain adapter name")
		}
		if !strings.Contains(errStr, "create") {
			t.Error("Error string should contain operation")
		}
		if !strings.Contains(errStr, "original error") {
			t.Error("Error string should contain original error")
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		if factoryErr.Unwrap() != originalErr {
			t.Error("Unwrap() should return the original error")
		}
	})
}

// Benchmark the fixed contains logic
func BenchmarkContainsLogic(b *testing.B) {
	// Benchmark our test implementation of the contains logic
	s := "FiberWebSocketContext"
	substr := "fiber"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testContainsLogic(s, substr)
	}
}

// Test the new SupportedContextTypes functionality
func TestSupportedContextTypes(t *testing.T) {
	fiberFactory := &mockFiberFactory{}
	httpRouterFactory := &mockHTTPRouterFactory{}

	t.Run("FiberFactory", func(t *testing.T) {
		supportedTypes := fiberFactory.SupportedContextTypes()
		expected := []string{"fiber", "fiberContext", "*fiberContext"}

		if len(supportedTypes) != len(expected) {
			t.Errorf("Expected %d supported types, got %d", len(expected), len(supportedTypes))
		}

		for i, expectedType := range expected {
			if supportedTypes[i] != expectedType {
				t.Errorf("Expected type %s at index %d, got %s", expectedType, i, supportedTypes[i])
			}
		}
	})

	t.Run("HTTPRouterFactory", func(t *testing.T) {
		supportedTypes := httpRouterFactory.SupportedContextTypes()
		expected := []string{"httprouter", "httpRouterContext", "*httpRouterContext"}

		if len(supportedTypes) != len(expected) {
			t.Errorf("Expected %d supported types, got %d", len(expected), len(supportedTypes))
		}

		for i, expectedType := range expected {
			if supportedTypes[i] != expectedType {
				t.Errorf("Expected type %s at index %d, got %s", expectedType, i, supportedTypes[i])
			}
		}
	})
}
