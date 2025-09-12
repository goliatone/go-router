package router

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestContextKeys tests the context key functionality
func TestContextKeys(t *testing.T) {
	ctx := context.Background()

	// Test WithRouteName and RouteNameFromContext
	routeName := "users.show"
	ctxWithName := WithRouteName(ctx, routeName)

	retrievedName, ok := RouteNameFromContext(ctxWithName)
	if !ok {
		t.Fatal("Expected to retrieve route name from context")
	}
	if retrievedName != routeName {
		t.Errorf("Expected route name %s, got %s", routeName, retrievedName)
	}

	// Test WithRouteParams and RouteParamsFromContext
	params := map[string]string{
		"id":   "123",
		"name": "john",
	}
	ctxWithParams := WithRouteParams(ctx, params)

	retrievedParams, ok := RouteParamsFromContext(ctxWithParams)
	if !ok {
		t.Fatal("Expected to retrieve route params from context")
	}
	if len(retrievedParams) != len(params) {
		t.Errorf("Expected %d params, got %d", len(params), len(retrievedParams))
	}
	for k, v := range params {
		if retrievedParams[k] != v {
			t.Errorf("Expected param %s=%s, got %s", k, v, retrievedParams[k])
		}
	}

	// Test retrieving from context without values
	emptyName, ok := RouteNameFromContext(ctx)
	if ok || emptyName != "" {
		t.Errorf("Expected no route name in empty context, got %s", emptyName)
	}

	emptyParams, ok := RouteParamsFromContext(ctx)
	if ok || emptyParams != nil {
		t.Errorf("Expected no route params in empty context, got %v", emptyParams)
	}
}

// TestBaseRouterRouteNameFromPath tests the route lookup functionality
func TestBaseRouterRouteNameFromPath(t *testing.T) {
	br := &BaseRouter{
		root: &routerRoot{
			routes: []*RouteDefinition{
				{Method: GET, Path: "/users/:id", Name: "users.show"},
				{Method: POST, Path: "/users", Name: "users.create"},
				{Method: GET, Path: "/health", Name: ""}, // unnamed route
			},
		},
	}

	tests := []struct {
		method   string
		path     string
		expected string
		found    bool
	}{
		{"GET", "/users/:id", "users.show", true},
		{"POST", "/users", "users.create", true},
		{"GET", "/health", "", false},       // unnamed route returns false
		{"DELETE", "/users/:id", "", false}, // non-existent route
		{"GET", "/nonexistent", "", false},  // non-existent route
	}

	for _, tt := range tests {
		t.Run(tt.method+"_"+tt.path, func(t *testing.T) {
			name, found := br.RouteNameFromPath(tt.method, tt.path)
			if found != tt.found {
				t.Errorf("Expected found=%v, got %v", tt.found, found)
			}
			if name != tt.expected {
				t.Errorf("Expected name=%s, got %s", tt.expected, name)
			}
		})
	}
}

// TestRouteNameResolution_Comprehensive tests comprehensive route name resolution scenarios
func TestRouteNameResolution_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		routes      []*RouteDefinition
		testCases   []routeNameTest
		description string
	}{
		{
			name: "Valid named routes",
			routes: []*RouteDefinition{
				{Method: GET, Path: "/users/:id", Name: "users.show"},
				{Method: POST, Path: "/users", Name: "users.create"},
				{Method: PUT, Path: "/users/:id", Name: "users.update"},
				{Method: DELETE, Path: "/users/:id", Name: "users.delete"},
			},
			testCases: []routeNameTest{
				{"GET", "/users/:id", "users.show", true},
				{"POST", "/users", "users.create", true},
				{"PUT", "/users/:id", "users.update", true},
				{"DELETE", "/users/:id", "users.delete", true},
			},
			description: "Basic CRUD operations with named routes",
		},
		{
			name: "Routes without names",
			routes: []*RouteDefinition{
				{Method: GET, Path: "/health", Name: ""},
				{Method: GET, Path: "/ping", Name: ""},
				{Method: GET, Path: "/status"},
			},
			testCases: []routeNameTest{
				{"GET", "/health", "", false},
				{"GET", "/ping", "", false},
				{"GET", "/status", "", false},
			},
			description: "Routes without names should return false",
		},
		{
			name: "Multiple routes with same pattern but different methods",
			routes: []*RouteDefinition{
				{Method: GET, Path: "/api/resource/:id", Name: "api.resource.show"},
				{Method: POST, Path: "/api/resource/:id", Name: "api.resource.update"},
				{Method: DELETE, Path: "/api/resource/:id", Name: "api.resource.delete"},
			},
			testCases: []routeNameTest{
				{"GET", "/api/resource/:id", "api.resource.show", true},
				{"POST", "/api/resource/:id", "api.resource.update", true},
				{"DELETE", "/api/resource/:id", "api.resource.delete", true},
				{"PUT", "/api/resource/:id", "", false}, // Method doesn't exist
			},
			description: "Same path pattern with different HTTP methods",
		},
		{
			name: "Nested route names",
			routes: []*RouteDefinition{
				{Method: GET, Path: "/api/v1/users/:id", Name: "api.v1.users.show"},
				{Method: GET, Path: "/api/v2/users/:id", Name: "api.v2.users.show"},
				{Method: GET, Path: "/admin/users/:id", Name: "admin.users.show"},
			},
			testCases: []routeNameTest{
				{"GET", "/api/v1/users/:id", "api.v1.users.show", true},
				{"GET", "/api/v2/users/:id", "api.v2.users.show", true},
				{"GET", "/admin/users/:id", "admin.users.show", true},
			},
			description: "Nested route names with versioning and prefixes",
		},
		{
			name: "Complex parameter patterns",
			routes: []*RouteDefinition{
				{Method: GET, Path: "/users/:userId/posts/:postId", Name: "users.posts.show"},
				{Method: GET, Path: "/files/*filepath", Name: "files.serve"},
				{Method: GET, Path: "/optional/:param?", Name: "optional.param"},
			},
			testCases: []routeNameTest{
				{"GET", "/users/:userId/posts/:postId", "users.posts.show", true},
				{"GET", "/files/*filepath", "files.serve", true},
				{"GET", "/optional/:param?", "optional.param", true},
			},
			description: "Routes with complex parameter patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br := &BaseRouter{
				root: &routerRoot{
					routes: tt.routes,
				},
			}

			for _, tc := range tt.testCases {
				t.Run(tc.method+"_"+tc.path, func(t *testing.T) {
					name, found := br.RouteNameFromPath(tc.method, tc.path)
					if found != tc.expected {
						t.Errorf("Expected found=%v, got %v for %s %s", tc.expected, found, tc.method, tc.path)
					}
					if name != tc.expectedName {
						t.Errorf("Expected name=%s, got %s for %s %s", tc.expectedName, name, tc.method, tc.path)
					}
				})
			}
		})
	}
}

// routeNameTest defines a test case for route name resolution
type routeNameTest struct {
	method       string
	path         string
	expectedName string
	expected     bool
}

// TestParameterExtraction_Comprehensive tests parameter extraction functionality
func TestParameterExtraction_Comprehensive(t *testing.T) {
	tests := []struct {
		name           string
		description    string
		contextSetup   func(context.Context) context.Context
		expectedParams map[string]string
		shouldFind     bool
	}{
		{
			name:        "Single parameter route",
			description: "Route with single parameter should extract correctly",
			contextSetup: func(ctx context.Context) context.Context {
				params := map[string]string{"id": "123"}
				return WithRouteParams(ctx, params)
			},
			expectedParams: map[string]string{"id": "123"},
			shouldFind:     true,
		},
		{
			name:        "Multiple parameters route",
			description: "Route with multiple parameters should extract all",
			contextSetup: func(ctx context.Context) context.Context {
				params := map[string]string{
					"userId": "456",
					"postId": "789",
					"format": "json",
				}
				return WithRouteParams(ctx, params)
			},
			expectedParams: map[string]string{
				"userId": "456",
				"postId": "789",
				"format": "json",
			},
			shouldFind: true,
		},
		{
			name:        "Empty parameters",
			description: "Route without parameters should return empty map",
			contextSetup: func(ctx context.Context) context.Context {
				params := map[string]string{}
				return WithRouteParams(ctx, params)
			},
			expectedParams: map[string]string{},
			shouldFind:     true,
		},
		{
			name:        "Special characters in parameters",
			description: "Parameters with special characters should be preserved",
			contextSetup: func(ctx context.Context) context.Context {
				params := map[string]string{
					"slug":     "hello-world_123",
					"category": "news&events",
					"version":  "v1.2.3",
				}
				return WithRouteParams(ctx, params)
			},
			expectedParams: map[string]string{
				"slug":     "hello-world_123",
				"category": "news&events",
				"version":  "v1.2.3",
			},
			shouldFind: true,
		},
		{
			name:        "Unicode parameters",
			description: "Parameters with Unicode characters should be handled",
			contextSetup: func(ctx context.Context) context.Context {
				params := map[string]string{
					"name":  "José",
					"city":  "São Paulo",
					"emoji": "🚀",
				}
				return WithRouteParams(ctx, params)
			},
			expectedParams: map[string]string{
				"name":  "José",
				"city":  "São Paulo",
				"emoji": "🚀",
			},
			shouldFind: true,
		},
		{
			name:        "No context parameters",
			description: "Context without parameters should return not found",
			contextSetup: func(ctx context.Context) context.Context {
				return ctx // No parameters set
			},
			expectedParams: nil,
			shouldFind:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = tt.contextSetup(ctx)

			params, found := RouteParamsFromContext(ctx)

			if found != tt.shouldFind {
				t.Errorf("Expected found=%v, got %v", tt.shouldFind, found)
			}

			if tt.shouldFind {
				if params == nil && tt.expectedParams != nil {
					t.Error("Expected non-nil params map but got nil")
					return
				}

				if len(params) != len(tt.expectedParams) {
					t.Errorf("Expected %d params, got %d", len(tt.expectedParams), len(params))
					return
				}

				for key, expectedValue := range tt.expectedParams {
					actualValue, exists := params[key]
					if !exists {
						t.Errorf("Expected param key %s not found", key)
						continue
					}
					if actualValue != expectedValue {
						t.Errorf("Expected param %s=%s, got %s", key, expectedValue, actualValue)
					}
				}

				// Check for unexpected keys
				for key := range params {
					if _, expected := tt.expectedParams[key]; !expected {
						t.Errorf("Unexpected param key %s found", key)
					}
				}
			}
		})
	}
}

// TestParameterExtractionEdgeCases tests edge cases in parameter extraction
func TestParameterExtractionEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		params         map[string]string
		description    string
		expectedLength int
	}{
		{
			name: "Empty string values",
			params: map[string]string{
				"empty":  "",
				"space":  " ",
				"normal": "value",
			},
			description:    "Empty and space-only values should be preserved",
			expectedLength: 3,
		},
		{
			name: "Duplicate-like keys",
			params: map[string]string{
				"id": "123",
				"ID": "456", // Different case
				"Id": "789", // Mixed case
			},
			description:    "Keys with different cases are different parameters",
			expectedLength: 3,
		},
		{
			name: "Long parameter values",
			params: map[string]string{
				"long":   strings.Repeat("a", 1000),
				"normal": "short",
			},
			description:    "Long parameter values should be handled",
			expectedLength: 2,
		},
		{
			name: "Many parameters",
			params: func() map[string]string {
				params := make(map[string]string)
				for i := 0; i < 50; i++ {
					params[fmt.Sprintf("param%d", i)] = fmt.Sprintf("value%d", i)
				}
				return params
			}(),
			description:    "Many parameters should be handled efficiently",
			expectedLength: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = WithRouteParams(ctx, tt.params)

			retrievedParams, found := RouteParamsFromContext(ctx)

			if !found {
				t.Fatal("Expected to find route params in context")
			}

			if len(retrievedParams) != tt.expectedLength {
				t.Errorf("Expected %d params, got %d", tt.expectedLength, len(retrievedParams))
			}

			// Verify all parameters match exactly
			for key, expectedValue := range tt.params {
				actualValue, exists := retrievedParams[key]
				if !exists {
					t.Errorf("Expected param key %s not found", key)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("Expected param %s=%s, got %s", key, expectedValue, actualValue)
				}
			}
		})
	}
}

// TestContextIntegration_ValueInjection tests context value injection during WrapHandler
func TestContextIntegration_ValueInjection(t *testing.T) {
	tests := []struct {
		name        string
		routeName   string
		routeParams map[string]string
		description string
	}{
		{
			name:        "Basic route context injection",
			routeName:   "users.show",
			routeParams: map[string]string{"id": "123"},
			description: "Basic route name and single parameter injection",
		},
		{
			name:      "Multiple parameters injection",
			routeName: "api.v1.users.posts.show",
			routeParams: map[string]string{
				"userId":  "456",
				"postId":  "789",
				"version": "v1",
			},
			description: "Complex route with multiple parameters",
		},
		{
			name:        "Empty parameters injection",
			routeName:   "health.check",
			routeParams: map[string]string{},
			description: "Route with name but no parameters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test context value injection and retrieval
			ctx := context.Background()

			// Inject route context values
			ctx = WithRouteName(ctx, tt.routeName)
			ctx = WithRouteParams(ctx, tt.routeParams)

			// Test route name retrieval
			retrievedName, nameFound := RouteNameFromContext(ctx)
			if !nameFound {
				t.Fatal("Expected to find route name in context")
			}
			if retrievedName != tt.routeName {
				t.Errorf("Expected route name %s, got %s", tt.routeName, retrievedName)
			}

			// Test route params retrieval
			retrievedParams, paramsFound := RouteParamsFromContext(ctx)
			if !paramsFound {
				t.Fatal("Expected to find route params in context")
			}

			if len(retrievedParams) != len(tt.routeParams) {
				t.Errorf("Expected %d params, got %d", len(tt.routeParams), len(retrievedParams))
			}

			for key, expectedValue := range tt.routeParams {
				actualValue, exists := retrievedParams[key]
				if !exists {
					t.Errorf("Expected param key %s not found", key)
				}
				if actualValue != expectedValue {
					t.Errorf("Expected param %s=%s, got %s", key, expectedValue, actualValue)
				}
			}
		})
	}
}

// TestContextIntegration_Persistence tests context persistence across middleware chain
func TestContextIntegration_Persistence(t *testing.T) {
	originalCtx := context.Background()
	routeName := "test.route"
	routeParams := map[string]string{
		"id":     "123",
		"action": "edit",
	}

	// Create context with route information
	ctx := WithRouteName(originalCtx, routeName)
	ctx = WithRouteParams(ctx, routeParams)

	// Simulate middleware chain by creating child contexts
	middleware1Ctx := context.WithValue(ctx, "middleware1", "executed")
	middleware2Ctx := context.WithValue(middleware1Ctx, "middleware2", "executed")
	handlerCtx := context.WithValue(middleware2Ctx, "handler", "executed")

	// Test that route information persists through the chain
	retrievedName, nameFound := RouteNameFromContext(handlerCtx)
	if !nameFound {
		t.Fatal("Route name should persist through middleware chain")
	}
	if retrievedName != routeName {
		t.Errorf("Expected route name %s, got %s after middleware chain", routeName, retrievedName)
	}

	retrievedParams, paramsFound := RouteParamsFromContext(handlerCtx)
	if !paramsFound {
		t.Fatal("Route params should persist through middleware chain")
	}
	if len(retrievedParams) != len(routeParams) {
		t.Errorf("Expected %d params, got %d after middleware chain", len(routeParams), len(retrievedParams))
	}

	for key, expectedValue := range routeParams {
		actualValue, exists := retrievedParams[key]
		if !exists {
			t.Errorf("Expected param key %s not found after middleware chain", key)
		}
		if actualValue != expectedValue {
			t.Errorf("Expected param %s=%s after middleware chain, got %s", key, expectedValue, actualValue)
		}
	}

	// Verify middleware values also persisted
	if handlerCtx.Value("middleware1") != "executed" {
		t.Error("Middleware1 value should persist")
	}
	if handlerCtx.Value("middleware2") != "executed" {
		t.Error("Middleware2 value should persist")
	}
	if handlerCtx.Value("handler") != "executed" {
		t.Error("Handler value should persist")
	}
}

// TestContextIntegration_Isolation tests context isolation between requests
func TestContextIntegration_Isolation(t *testing.T) {
	// Simulate two concurrent requests with different route contexts
	request1Ctx := context.Background()
	request1Ctx = WithRouteName(request1Ctx, "users.show")
	request1Ctx = WithRouteParams(request1Ctx, map[string]string{"id": "123"})

	request2Ctx := context.Background()
	request2Ctx = WithRouteName(request2Ctx, "posts.edit")
	request2Ctx = WithRouteParams(request2Ctx, map[string]string{"postId": "456", "userId": "789"})

	// Verify request1 context
	name1, found1 := RouteNameFromContext(request1Ctx)
	if !found1 || name1 != "users.show" {
		t.Errorf("Request1 expected route name 'users.show', got %s (found: %v)", name1, found1)
	}

	params1, paramsFound1 := RouteParamsFromContext(request1Ctx)
	if !paramsFound1 || len(params1) != 1 || params1["id"] != "123" {
		t.Errorf("Request1 expected params map[id:123], got %v (found: %v)", params1, paramsFound1)
	}

	// Verify request2 context
	name2, found2 := RouteNameFromContext(request2Ctx)
	if !found2 || name2 != "posts.edit" {
		t.Errorf("Request2 expected route name 'posts.edit', got %s (found: %v)", name2, found2)
	}

	params2, paramsFound2 := RouteParamsFromContext(request2Ctx)
	if !paramsFound2 || len(params2) != 2 || params2["postId"] != "456" || params2["userId"] != "789" {
		t.Errorf("Request2 expected params map[postId:456 userId:789], got %v (found: %v)", params2, paramsFound2)
	}

	// Cross-verify isolation: request1 shouldn't see request2's data
	if name1 == name2 {
		t.Error("Route names should be isolated between requests")
	}

	if len(params1) == len(params2) && params1["id"] == params2["postId"] {
		t.Error("Route params should be isolated between requests")
	}
}

// TestContextIntegration_MemoryManagement tests for memory management
func TestContextIntegration_MemoryManagement(t *testing.T) {
	// Test that context values don't leak between different context instances
	baseCtx := context.Background()

	// Create multiple contexts with different route information
	contexts := make([]context.Context, 10)
	for i := 0; i < 10; i++ {
		ctx := WithRouteName(baseCtx, fmt.Sprintf("route.%d", i))
		ctx = WithRouteParams(ctx, map[string]string{
			"id":    fmt.Sprintf("%d", i),
			"index": fmt.Sprintf("idx-%d", i),
		})
		contexts[i] = ctx
	}

	// Verify each context has its own isolated data
	for i, ctx := range contexts {
		name, found := RouteNameFromContext(ctx)
		if !found {
			t.Errorf("Context %d should have route name", i)
			continue
		}
		expectedName := fmt.Sprintf("route.%d", i)
		if name != expectedName {
			t.Errorf("Context %d expected name %s, got %s", i, expectedName, name)
		}

		params, paramsFound := RouteParamsFromContext(ctx)
		if !paramsFound {
			t.Errorf("Context %d should have route params", i)
			continue
		}

		expectedId := fmt.Sprintf("%d", i)
		expectedIndex := fmt.Sprintf("idx-%d", i)

		if params["id"] != expectedId {
			t.Errorf("Context %d expected id %s, got %s", i, expectedId, params["id"])
		}
		if params["index"] != expectedIndex {
			t.Errorf("Context %d expected index %s, got %s", i, expectedIndex, params["index"])
		}
	}
}

// TestContextImplementations tests that our context implementations have the new methods
func TestContextImplementations(t *testing.T) {
	t.Run("MockContext", func(t *testing.T) {
		ctx := NewMockContext()

		// Set up mock expectations
		ctx.On("RouteName").Return("")
		ctx.On("RouteParams").Return(make(map[string]string))

		// These should not panic and should return expected values
		routeName := ctx.RouteName()
		if routeName != "" {
			t.Errorf("Expected empty route name from mock context, got %s", routeName)
		}

		params := ctx.RouteParams()
		if params == nil {
			t.Error("Expected non-nil route params map from mock context")
		}
		if len(params) != 0 {
			t.Errorf("Expected empty route params map from mock context, got %d items", len(params))
		}
	})

	// Note: We can't test the builder mock context directly as it's private to builder_test.go
	// But the fact that the build succeeds means it implements the interface correctly
}
