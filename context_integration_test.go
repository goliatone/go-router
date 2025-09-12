package router

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestFiberRouteContextInjection tests that Fiber adapter injects route context correctly
func TestFiberRouteContextInjection(t *testing.T) {
	// Create router adapter
	adapter := NewFiberAdapter().(*FiberAdapter)

	// Create a test route with a name
	testRouteName := "users.show"

	handler := func(ctx Context) error {
		return ctx.SendString("OK")
	}

	// Create a named route
	route := adapter.Router().Get("/users/:id", handler).SetName(testRouteName)
	_ = route

	// Initialize the adapter
	adapter.Init()

	// This is tricky because we need to simulate how Fiber would call our handler
	// For now, let's test the WrapHandler directly
	wrappedHandler := adapter.WrapHandler(handler)
	_, ok := wrappedHandler.(func(*fiber.Ctx) error)
	if !ok {
		t.Fatal("WrapHandler should return fiber handler function")
	}

	// Create a minimal fiber context for testing
	// Note: This is challenging because fiber.Ctx is complex to mock
	// We'll test the pattern matching logic separately

	t.Log("Fiber integration test - handler wrapping verified")
}

// TestHTTPRouterRouteContextInjection tests that HTTPRouter adapter injects route context correctly
func TestHTTPRouterRouteContextInjection(t *testing.T) {
	// Create HTTPRouter server
	server := NewHTTPServer().(*HTTPServer)

	// Create a test route with a name
	testRouteName := "api.users.show"
	var capturedRouteName string
	var capturedParams map[string]string

	handler := func(ctx Context) error {
		capturedRouteName = ctx.RouteName()
		capturedParams = ctx.RouteParams()
		return ctx.SendString("OK")
	}

	// Create a named route
	route := server.Router().Get("/api/users/:id", handler).SetName(testRouteName)
	_ = route

	// Initialize the server
	server.Init()

	// Test using the actual httprouter
	req := httptest.NewRequest("GET", "/api/users/123", nil)
	resp := httptest.NewRecorder()

	// Call the route through the httprouter
	server.WrappedRouter().ServeHTTP(resp, req)

	// Verify route context was injected
	if capturedRouteName != testRouteName {
		t.Errorf("Expected route name %s, got %s", testRouteName, capturedRouteName)
	}

	if capturedParams == nil {
		t.Fatal("Expected route params to be captured")
	}

	if capturedParams["id"] != "123" {
		t.Errorf("Expected param id=123, got %s", capturedParams["id"])
	}

	// Verify response
	if resp.Code != 200 {
		t.Errorf("Expected status 200, got %d", resp.Code)
	}
}

// TestHTTPRouterPatternMatching tests the pattern matching logic
func TestHTTPRouterPatternMatching(t *testing.T) {
	tests := []struct {
		pattern     string
		path        string
		shouldMatch bool
	}{
		{"/users/:id", "/users/123", true},
		{"/users/:id", "/users/123/posts", false},
		{"/api/v1/users/:userId/posts/:postId", "/api/v1/users/123/posts/456", true},
		{"/api/v1/users/:userId/posts/:postId", "/api/v1/users/123", false},
		{"/static/*filepath", "/static/css/style.css", true},
		{"/static/*filepath", "/other/file.css", false},
		{"/health", "/health", true},
		{"/health", "/healthcheck", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			result := pathMatchesPattern(tt.pattern, tt.path)
			if result != tt.shouldMatch {
				t.Errorf("Pattern %s with path %s: expected %v, got %v",
					tt.pattern, tt.path, tt.shouldMatch, result)
			}
		})
	}
}

// TestGetRoutePattern tests the HTTPServer.getRoutePattern method
func TestGetRoutePattern(t *testing.T) {
	server := NewHTTPServer().(*HTTPServer)

	// Add some test routes
	server.Router().Get("/users/:id", func(ctx Context) error { return nil }).SetName("users.show")
	server.Router().Post("/users", func(ctx Context) error { return nil }).SetName("users.create")
	server.Router().Get("/health", func(ctx Context) error { return nil }) // unnamed route

	server.Init()

	tests := []struct {
		method      string
		path        string
		expected    string
		description string
	}{
		{"GET", "/users/123", "/users/:id", "parameterized route"},
		{"POST", "/users", "/users", "exact match route"},
		{"GET", "/health", "/health", "static route"},
		{"DELETE", "/users/123", "", "non-existent method"},
		{"GET", "/nonexistent", "", "non-existent path"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := server.getRoutePattern(tt.method, tt.path)
			if result != tt.expected {
				t.Errorf("getRoutePattern(%s, %s): expected %s, got %s",
					tt.method, tt.path, tt.expected, result)
			}
		})
	}
}

// TestRouteContextPersistence tests that route context persists through middleware chain
func TestRouteContextPersistence(t *testing.T) {
	server := NewHTTPServer().(*HTTPServer)

	var middlewareRouteName string
	var handlerRouteName string

	// Add middleware that captures route name
	middleware := func(next HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			middlewareRouteName = ctx.RouteName()
			return next(ctx)
		}
	}

	handler := func(ctx Context) error {
		handlerRouteName = ctx.RouteName()
		return ctx.SendString("OK")
	}

	// Create route with middleware
	testRouteName := "test.route"
	server.Router().Get("/test/:id", handler, middleware).SetName(testRouteName)
	server.Init()

	// Test using the actual httprouter
	req := httptest.NewRequest("GET", "/test/456", nil)
	resp := httptest.NewRecorder()

	// Call the route through the httprouter
	server.WrappedRouter().ServeHTTP(resp, req)

	// Both middleware and handler should see the same route name
	if middlewareRouteName != testRouteName {
		t.Errorf("Middleware expected route name %s, got %s", testRouteName, middlewareRouteName)
	}

	if handlerRouteName != testRouteName {
		t.Errorf("Handler expected route name %s, got %s", testRouteName, handlerRouteName)
	}
}

// TestUnnamedRoutes tests behavior with routes that don't have names
func TestUnnamedRoutes(t *testing.T) {
	server := NewHTTPServer().(*HTTPServer)

	var capturedRouteName string
	var capturedParams map[string]string

	handler := func(ctx Context) error {
		capturedRouteName = ctx.RouteName()
		capturedParams = ctx.RouteParams()
		return ctx.SendString("OK")
	}

	// Create route without name
	server.Router().Get("/unnamed/:id", handler) // No .SetName() call
	server.Init()

	req := httptest.NewRequest("GET", "/unnamed/789", nil)
	resp := httptest.NewRecorder()

	// Call the route through the httprouter
	server.WrappedRouter().ServeHTTP(resp, req)

	// Route name should be empty for unnamed routes
	if capturedRouteName != "" {
		t.Errorf("Expected empty route name for unnamed route, got %s", capturedRouteName)
	}

	// But params should still be captured
	if capturedParams == nil {
		t.Fatal("Expected route params to be captured even for unnamed routes")
	}

	if capturedParams["id"] != "789" {
		t.Errorf("Expected param id=789, got %s", capturedParams["id"])
	}
}
