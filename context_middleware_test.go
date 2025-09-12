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
