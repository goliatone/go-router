package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCrossAdapterConsistency_RouteName tests that RouteName() works consistently across adapters
func TestCrossAdapterConsistency_RouteName(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		path      string
		routeName string
		param     string
		value     string
	}{
		{
			name:      "Simple named route",
			method:    "GET",
			path:      "/users/:id",
			routeName: "users.show",
			param:     "id",
			value:     "123",
		},
		{
			name:      "Complex nested route", 
			method:    "POST",
			path:      "/api/v1/users/:userId/posts/:postId",
			routeName: "api.v1.users.posts.update",
			param:     "userId",
			value:     "456",
		},
		{
			name:      "Route with special characters",
			method:    "PUT",
			path:      "/files/:filename",
			routeName: "files.update",
			param:     "filename",
			value:     "test-file_123.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test with Fiber adapter
			t.Run("FiberAdapter", func(t *testing.T) {
				adapter := NewFiberAdapter()
				router := adapter.Router()

				var capturedRouteName string
				var capturedParams map[string]string

				handler := func(ctx Context) error {
					capturedRouteName = ctx.RouteName()
					capturedParams = ctx.RouteParams()
					return ctx.Send([]byte("OK"))
				}

				route := router.Handle(HTTPMethod(tt.method), tt.path, handler)
				route.SetName(tt.routeName)

				app := adapter.WrappedRouter()
				
				// Build test URL
				testPath := strings.ReplaceAll(tt.path, ":"+tt.param, tt.value)
				req := httptest.NewRequest(tt.method, testPath, nil)
				
				resp, err := app.Test(req)
				if err != nil {
					t.Fatalf("Fiber test failed: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Fiber expected status 200, got %d", resp.StatusCode)
				}

				if capturedRouteName != tt.routeName {
					t.Errorf("Fiber expected route name %s, got %s", tt.routeName, capturedRouteName)
				}

				if capturedParams == nil {
					t.Fatal("Fiber expected non-nil route params")
				}

				if capturedParams[tt.param] != tt.value {
					t.Errorf("Fiber expected param %s=%s, got %s", tt.param, tt.value, capturedParams[tt.param])
				}
			})

			// Test with HTTPRouter adapter
			t.Run("HTTPRouterAdapter", func(t *testing.T) {
				adapter := NewHTTPServer()
				router := adapter.Router()

				var capturedRouteName string
				var capturedParams map[string]string

				handler := func(ctx Context) error {
					capturedRouteName = ctx.RouteName()
					capturedParams = ctx.RouteParams()
					return ctx.Send([]byte("OK"))
				}

				route := router.Handle(HTTPMethod(tt.method), tt.path, handler)
				route.SetName(tt.routeName)

				server := httptest.NewServer(adapter.WrappedRouter())
				defer server.Close()

				// Build test URL
				testPath := strings.ReplaceAll(tt.path, ":"+tt.param, tt.value)
				url := server.URL + testPath

				req, err := http.NewRequest(tt.method, url, nil)
				if err != nil {
					t.Fatalf("HTTPRouter request creation failed: %v", err)
				}

				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("HTTPRouter test failed: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("HTTPRouter expected status 200, got %d", resp.StatusCode)
				}

				if capturedRouteName != tt.routeName {
					t.Errorf("HTTPRouter expected route name %s, got %s", tt.routeName, capturedRouteName)
				}

				if capturedParams == nil {
					t.Fatal("HTTPRouter expected non-nil route params")
				}

				if capturedParams[tt.param] != tt.value {
					t.Errorf("HTTPRouter expected param %s=%s, got %s", tt.param, tt.value, capturedParams[tt.param])
				}
			})
		})
	}
}

// TestCrossAdapterConsistency_RouteParams tests that RouteParams() works consistently
func TestCrossAdapterConsistency_RouteParams(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		routeName      string
		testPath       string
		expectedParams map[string]string
	}{
		{
			name:      "Single parameter",
			method:    "GET",
			path:      "/users/:id",
			routeName: "users.show",
			testPath:  "/users/123",
			expectedParams: map[string]string{
				"id": "123",
			},
		},
		{
			name:      "Multiple parameters",
			method:    "GET", 
			path:      "/users/:userId/posts/:postId",
			routeName: "users.posts.show",
			testPath:  "/users/456/posts/789",
			expectedParams: map[string]string{
				"userId": "456",
				"postId": "789",
			},
		},
		{
			name:           "No parameters",
			method:         "GET",
			path:           "/health",
			routeName:      "health.check",
			testPath:       "/health",
			expectedParams: map[string]string{},
		},
		{
			name:      "Parameters with special characters",
			method:    "GET",
			path:      "/files/:filename",
			routeName: "files.show",
			testPath:  "/files/test-file_123.txt",
			expectedParams: map[string]string{
				"filename": "test-file_123.txt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fiberParams, httpRouterParams map[string]string

			// Test Fiber adapter
			t.Run("Fiber", func(t *testing.T) {
				adapter := NewFiberAdapter()
				router := adapter.Router()

				handler := func(ctx Context) error {
					fiberParams = ctx.RouteParams()
					return ctx.Send([]byte("OK"))
				}

				route := router.Handle(HTTPMethod(tt.method), tt.path, handler)
				route.SetName(tt.routeName)

				app := adapter.WrappedRouter()
				req := httptest.NewRequest(tt.method, tt.testPath, nil)
				
				_, err := app.Test(req)
				if err != nil {
					t.Fatalf("Fiber test failed: %v", err)
				}

				// Validate Fiber params
				if len(fiberParams) != len(tt.expectedParams) {
					t.Errorf("Fiber expected %d params, got %d", len(tt.expectedParams), len(fiberParams))
				}

				for key, expectedValue := range tt.expectedParams {
					actualValue, exists := fiberParams[key]
					if !exists {
						t.Errorf("Fiber expected param key %s not found", key)
					} else if actualValue != expectedValue {
						t.Errorf("Fiber expected param %s=%s, got %s", key, expectedValue, actualValue)
					}
				}
			})

			// Test HTTPRouter adapter
			t.Run("HTTPRouter", func(t *testing.T) {
				adapter := NewHTTPServer()
				router := adapter.Router()

				handler := func(ctx Context) error {
					httpRouterParams = ctx.RouteParams()
					return ctx.Send([]byte("OK"))
				}

				route := router.Handle(HTTPMethod(tt.method), tt.path, handler)
				route.SetName(tt.routeName)

				server := httptest.NewServer(adapter.WrappedRouter())
				defer server.Close()

				req, err := http.NewRequest(tt.method, server.URL+tt.testPath, nil)
				if err != nil {
					t.Fatalf("HTTPRouter request creation failed: %v", err)
				}

				client := &http.Client{}
				_, err = client.Do(req)
				if err != nil {
					t.Fatalf("HTTPRouter test failed: %v", err)
				}

				// Validate HTTPRouter params
				if len(httpRouterParams) != len(tt.expectedParams) {
					t.Errorf("HTTPRouter expected %d params, got %d", len(tt.expectedParams), len(httpRouterParams))
				}

				for key, expectedValue := range tt.expectedParams {
					actualValue, exists := httpRouterParams[key]
					if !exists {
						t.Errorf("HTTPRouter expected param key %s not found", key)
					} else if actualValue != expectedValue {
						t.Errorf("HTTPRouter expected param %s=%s, got %s", key, expectedValue, actualValue)
					}
				}
			})

			// Compare results between adapters
			t.Run("CrossAdapterComparison", func(t *testing.T) {
				if len(fiberParams) != len(httpRouterParams) {
					t.Errorf("Adapter params length mismatch: Fiber=%d, HTTPRouter=%d", 
						len(fiberParams), len(httpRouterParams))
				}

				// Compare each parameter
				for key, fiberValue := range fiberParams {
					httpRouterValue, exists := httpRouterParams[key]
					if !exists {
						t.Errorf("Parameter %s exists in Fiber but not in HTTPRouter", key)
					} else if fiberValue != httpRouterValue {
						t.Errorf("Parameter %s value mismatch: Fiber=%s, HTTPRouter=%s", 
							key, fiberValue, httpRouterValue)
					}
				}

				for key := range httpRouterParams {
					if _, exists := fiberParams[key]; !exists {
						t.Errorf("Parameter %s exists in HTTPRouter but not in Fiber", key)
					}
				}
			})
		})
	}
}

// TestCrossAdapterConsistency_MiddlewareIntegration tests middleware chain consistency
func TestCrossAdapterConsistency_MiddlewareIntegration(t *testing.T) {
	routeName := "middleware.test"
	testPath := "/middleware/123"

	middlewareTest := func(t *testing.T, adapterName string, setupAndTest func() error) {
		t.Run(adapterName, func(t *testing.T) {
			err := setupAndTest()
			if err != nil {
				t.Fatalf("%s middleware test failed: %v", adapterName, err)
			}
		})
	}

	// Test that route context is available in middleware
	middlewareTest(t, "Fiber", func() error {
		adapter := NewFiberAdapter()
		router := adapter.Router()

		var middlewareRouteName string
		var middlewareParams map[string]string
		var handlerRouteName string
		var handlerParams map[string]string

		middleware := func(next HandlerFunc) HandlerFunc {
			return func(ctx Context) error {
				middlewareRouteName = ctx.RouteName()
				middlewareParams = ctx.RouteParams()
				return next(ctx)
			}
		}

		handler := func(ctx Context) error {
			handlerRouteName = ctx.RouteName()
			handlerParams = ctx.RouteParams()
			return ctx.Send([]byte("OK"))
		}

		route := router.Handle(GET, "/middleware/:id", handler, middleware)
		route.SetName(routeName)

		app := adapter.WrappedRouter()
		req := httptest.NewRequest("GET", testPath, nil)
		
		resp, err := app.Test(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// Validate middleware had access to route context
		if middlewareRouteName != routeName {
			return fmt.Errorf("middleware expected route name %s, got %s", routeName, middlewareRouteName)
		}
		if middlewareParams["id"] != "123" {
			return fmt.Errorf("middleware expected id=123, got %s", middlewareParams["id"])
		}

		// Validate handler had access to route context
		if handlerRouteName != routeName {
			return fmt.Errorf("handler expected route name %s, got %s", routeName, handlerRouteName)
		}
		if handlerParams["id"] != "123" {
			return fmt.Errorf("handler expected id=123, got %s", handlerParams["id"])
		}

		return nil
	})

	middlewareTest(t, "HTTPRouter", func() error {
		adapter := NewHTTPServer()
		router := adapter.Router()

		var middlewareRouteName string
		var middlewareParams map[string]string
		var handlerRouteName string
		var handlerParams map[string]string

		middleware := func(next HandlerFunc) HandlerFunc {
			return func(ctx Context) error {
				middlewareRouteName = ctx.RouteName()
				middlewareParams = ctx.RouteParams()
				return next(ctx)
			}
		}

		handler := func(ctx Context) error {
			handlerRouteName = ctx.RouteName()
			handlerParams = ctx.RouteParams()
			return ctx.Send([]byte("OK"))
		}

		route := router.Handle(GET, "/middleware/:id", handler, middleware)
		route.SetName(routeName)

		server := httptest.NewServer(adapter.WrappedRouter())
		defer server.Close()

		req, err := http.NewRequest("GET", server.URL+testPath, nil)
		if err != nil {
			return err
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// Validate middleware had access to route context
		if middlewareRouteName != routeName {
			return fmt.Errorf("middleware expected route name %s, got %s", routeName, middlewareRouteName)
		}
		if middlewareParams["id"] != "123" {
			return fmt.Errorf("middleware expected id=123, got %s", middlewareParams["id"])
		}

		// Validate handler had access to route context
		if handlerRouteName != routeName {
			return fmt.Errorf("handler expected route name %s, got %s", routeName, handlerRouteName)
		}
		if handlerParams["id"] != "123" {
			return fmt.Errorf("handler expected id=123, got %s", handlerParams["id"])
		}

		return nil
	})
}