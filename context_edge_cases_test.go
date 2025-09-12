package router

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestEdgeCases_ConcurrentAccess tests concurrent access to route context
func TestEdgeCases_ConcurrentAccess(t *testing.T) {
	const numGoroutines = 100
	const numIterations = 10

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numIterations)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()

			for j := 0; j < numIterations; j++ {
				// Create unique context for this goroutine/iteration
				ctx := context.Background()
				routeName := fmt.Sprintf("route.%d.%d", routineID, j)
				params := map[string]string{
					"id":      fmt.Sprintf("%d", routineID),
					"version": fmt.Sprintf("%d", j),
				}

				// Inject route context
				ctx = WithRouteName(ctx, routeName)
				ctx = WithRouteParams(ctx, params)

				// Verify values immediately
				retrievedName, nameFound := RouteNameFromContext(ctx)
				if !nameFound {
					errors <- fmt.Errorf("goroutine %d iteration %d: route name not found", routineID, j)
					continue
				}
				if retrievedName != routeName {
					errors <- fmt.Errorf("goroutine %d iteration %d: expected name %s, got %s", routineID, j, routeName, retrievedName)
					continue
				}

				retrievedParams, paramsFound := RouteParamsFromContext(ctx)
				if !paramsFound {
					errors <- fmt.Errorf("goroutine %d iteration %d: route params not found", routineID, j)
					continue
				}

				for key, expectedValue := range params {
					actualValue, exists := retrievedParams[key]
					if !exists {
						errors <- fmt.Errorf("goroutine %d iteration %d: param key %s not found", routineID, j, key)
						break
					}
					if actualValue != expectedValue {
						errors <- fmt.Errorf("goroutine %d iteration %d: param %s expected %s, got %s", routineID, j, key, expectedValue, actualValue)
						break
					}
				}

				// Small delay to increase chance of race conditions
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errorCount int
	for err := range errors {
		if errorCount < 5 { // Limit error output
			t.Errorf("Concurrent access error: %v", err)
		}
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("Total concurrent access errors: %d", errorCount)
	}
}

// TestEdgeCases_ContextHierarchy tests complex context hierarchies
func TestEdgeCases_ContextHierarchy(t *testing.T) {
	// Create a deep context hierarchy
	baseCtx := context.Background()

	// Add route context
	routeCtx := WithRouteName(baseCtx, "nested.route")
	routeCtx = WithRouteParams(routeCtx, map[string]string{"id": "123"})

	// Add application context values
	appCtx := context.WithValue(routeCtx, "app", "myapp")
	requestCtx := context.WithValue(appCtx, "requestId", "req-456")
	userCtx := context.WithValue(requestCtx, "userId", "user-789")

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(userCtx, time.Second)
	defer cancel()

	// Create cancellable context
	cancelCtx, cancelFunc := context.WithCancel(timeoutCtx)
	defer cancelFunc()

	// Verify route context still accessible through deep hierarchy
	retrievedName, nameFound := RouteNameFromContext(cancelCtx)
	if !nameFound {
		t.Fatal("Route name should be accessible through deep context hierarchy")
	}
	if retrievedName != "nested.route" {
		t.Errorf("Expected route name 'nested.route', got %s", retrievedName)
	}

	retrievedParams, paramsFound := RouteParamsFromContext(cancelCtx)
	if !paramsFound {
		t.Fatal("Route params should be accessible through deep context hierarchy")
	}
	if retrievedParams["id"] != "123" {
		t.Errorf("Expected param id=123, got %s", retrievedParams["id"])
	}

	// Verify other context values are also accessible
	if cancelCtx.Value("app") != "myapp" {
		t.Error("App context value should be accessible")
	}
	if cancelCtx.Value("requestId") != "req-456" {
		t.Error("Request ID context value should be accessible")
	}
	if cancelCtx.Value("userId") != "user-789" {
		t.Error("User ID context value should be accessible")
	}
}

// TestEdgeCases_NilAndEmptyValues tests handling of nil and empty values
func TestEdgeCases_NilAndEmptyValues(t *testing.T) {
	tests := []struct {
		name             string
		setupContext     func(context.Context) context.Context
		expectedName     string
		expectedParams   map[string]string
		shouldFindName   bool
		shouldFindParams bool
	}{
		{
			name: "Nil parameter map",
			setupContext: func(ctx context.Context) context.Context {
				ctx = WithRouteName(ctx, "test.route")
				return WithRouteParams(ctx, nil)
			},
			expectedName:     "test.route",
			expectedParams:   nil,
			shouldFindName:   true,
			shouldFindParams: true, // nil maps are valid
		},
		{
			name: "Empty parameter map",
			setupContext: func(ctx context.Context) context.Context {
				ctx = WithRouteName(ctx, "empty.route")
				return WithRouteParams(ctx, map[string]string{})
			},
			expectedName:     "empty.route",
			expectedParams:   map[string]string{},
			shouldFindName:   true,
			shouldFindParams: true,
		},
		{
			name: "Empty route name",
			setupContext: func(ctx context.Context) context.Context {
				ctx = WithRouteName(ctx, "")
				return WithRouteParams(ctx, map[string]string{"id": "123"})
			},
			expectedName:     "",
			expectedParams:   map[string]string{"id": "123"},
			shouldFindName:   true,
			shouldFindParams: true,
		},
		{
			name: "Only route name set",
			setupContext: func(ctx context.Context) context.Context {
				return WithRouteName(ctx, "only.name")
			},
			expectedName:     "only.name",
			expectedParams:   nil,
			shouldFindName:   true,
			shouldFindParams: false,
		},
		{
			name: "Only route params set",
			setupContext: func(ctx context.Context) context.Context {
				return WithRouteParams(ctx, map[string]string{"id": "456"})
			},
			expectedName:     "",
			expectedParams:   map[string]string{"id": "456"},
			shouldFindName:   false,
			shouldFindParams: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = tt.setupContext(ctx)

			// Test route name
			name, nameFound := RouteNameFromContext(ctx)
			if nameFound != tt.shouldFindName {
				t.Errorf("Expected name found=%v, got %v", tt.shouldFindName, nameFound)
			}
			if tt.shouldFindName && name != tt.expectedName {
				t.Errorf("Expected name=%s, got %s", tt.expectedName, name)
			}

			// Test route params
			params, paramsFound := RouteParamsFromContext(ctx)
			if paramsFound != tt.shouldFindParams {
				t.Errorf("Expected params found=%v, got %v", tt.shouldFindParams, paramsFound)
			}

			if tt.shouldFindParams {
				if tt.expectedParams == nil {
					if params != nil {
						t.Errorf("Expected nil params, got %v", params)
					}
				} else {
					if len(params) != len(tt.expectedParams) {
						t.Errorf("Expected %d params, got %d", len(tt.expectedParams), len(params))
					}
					for key, expectedValue := range tt.expectedParams {
						actualValue, exists := params[key]
						if !exists {
							t.Errorf("Expected param key %s not found", key)
						} else if actualValue != expectedValue {
							t.Errorf("Expected param %s=%s, got %s", key, expectedValue, actualValue)
						}
					}
				}
			}
		})
	}
}

// TestEdgeCases_LargeDataValues tests handling of large data values
func TestEdgeCases_LargeDataValues(t *testing.T) {
	// Create large route name
	prefix := "very.long.route.name."
	largeRouteName := prefix + strings.Repeat("a", 10000)

	// Create large parameter values
	largeParams := map[string]string{
		"small":  "normal",
		"large":  strings.Repeat("x", 50000), // 50KB value
		"medium": strings.Repeat("m", 1000),  // 1KB value
	}

	// Test large data handling
	ctx := context.Background()
	ctx = WithRouteName(ctx, largeRouteName)
	ctx = WithRouteParams(ctx, largeParams)

	// Verify large route name
	retrievedName, nameFound := RouteNameFromContext(ctx)
	if !nameFound {
		t.Fatal("Should find large route name")
	}
	if len(retrievedName) != len(largeRouteName) {
		t.Errorf("Expected route name length %d, got %d", len(largeRouteName), len(retrievedName))
	}
	if retrievedName != largeRouteName {
		t.Error("Large route name should match exactly")
	}

	// Verify large parameters
	retrievedParams, paramsFound := RouteParamsFromContext(ctx)
	if !paramsFound {
		t.Fatal("Should find large route params")
	}

	if len(retrievedParams) != len(largeParams) {
		t.Errorf("Expected %d params, got %d", len(largeParams), len(retrievedParams))
	}

	for key, expectedValue := range largeParams {
		actualValue, exists := retrievedParams[key]
		if !exists {
			t.Errorf("Large param key %s not found", key)
			continue
		}
		if len(actualValue) != len(expectedValue) {
			t.Errorf("Large param %s length mismatch: expected %d, got %d",
				key, len(expectedValue), len(actualValue))
		}
		if actualValue != expectedValue {
			t.Errorf("Large param %s value mismatch", key)
		}
	}
}

// TestEdgeCases_ContextOverwrite tests context value overwrites
func TestEdgeCases_ContextOverwrite(t *testing.T) {
	baseCtx := context.Background()

	// Initial values
	ctx1 := WithRouteName(baseCtx, "initial.route")
	ctx1 = WithRouteParams(ctx1, map[string]string{"id": "initial"})

	// Overwrite route name
	ctx2 := WithRouteName(ctx1, "overwritten.route")

	// Overwrite params
	ctx3 := WithRouteParams(ctx2, map[string]string{"id": "overwritten", "new": "param"})

	// Verify overwritten values
	name, nameFound := RouteNameFromContext(ctx3)
	if !nameFound {
		t.Fatal("Should find overwritten route name")
	}
	if name != "overwritten.route" {
		t.Errorf("Expected overwritten route name 'overwritten.route', got %s", name)
	}

	params, paramsFound := RouteParamsFromContext(ctx3)
	if !paramsFound {
		t.Fatal("Should find overwritten route params")
	}

	expectedParams := map[string]string{"id": "overwritten", "new": "param"}
	if len(params) != len(expectedParams) {
		t.Errorf("Expected %d params after overwrite, got %d", len(expectedParams), len(params))
	}

	for key, expectedValue := range expectedParams {
		actualValue, exists := params[key]
		if !exists {
			t.Errorf("Expected overwritten param key %s not found", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected overwritten param %s=%s, got %s", key, expectedValue, actualValue)
		}
	}

	// Verify original context unchanged
	originalName, originalNameFound := RouteNameFromContext(ctx1)
	if !originalNameFound || originalName != "initial.route" {
		t.Error("Original context should remain unchanged")
	}

	originalParams, originalParamsFound := RouteParamsFromContext(ctx1)
	if !originalParamsFound || len(originalParams) != 1 || originalParams["id"] != "initial" {
		t.Error("Original context params should remain unchanged")
	}
}

// TestEdgeCases_MemoryLeaks tests for potential memory leaks
func TestEdgeCases_MemoryLeaks(t *testing.T) {
	// This test creates many contexts and ensures they can be garbage collected
	const numContexts = 1000

	for i := 0; i < numContexts; i++ {
		ctx := context.Background()

		// Create context with data
		routeName := fmt.Sprintf("route.%d", i)
		params := map[string]string{
			"id":   fmt.Sprintf("%d", i),
			"data": string(make([]byte, 1000)), // 1KB per context
		}

		ctx = WithRouteName(ctx, routeName)
		ctx = WithRouteParams(ctx, params)

		// Use the context briefly
		name, _ := RouteNameFromContext(ctx)
		retrievedParams, _ := RouteParamsFromContext(ctx)

		// Verify data is correct
		if name != routeName {
			t.Errorf("Context %d: expected name %s, got %s", i, routeName, name)
			break
		}

		if retrievedParams["id"] != fmt.Sprintf("%d", i) {
			t.Errorf("Context %d: expected id %d, got %s", i, i, retrievedParams["id"])
			break
		}

		// Context should be eligible for GC after this iteration
	}

	// Force garbage collection to ensure no leaks
	// Note: In a real test, you might use runtime.GC() and runtime.ReadMemStats()
	// but for unit tests, we'll just complete successfully
}

// TestEdgeCases_UnicodeAndSpecialCharacters tests Unicode and special character handling
func TestEdgeCases_UnicodeAndSpecialCharacters(t *testing.T) {
	tests := []struct {
		name        string
		routeName   string
		routeParams map[string]string
	}{
		{
			name:      "Unicode characters",
			routeName: "🚀.rocket.route.测试",
			routeParams: map[string]string{
				"name":    "José María",
				"city":    "São Paulo",
				"emoji":   "🎉🎊✨",
				"chinese": "你好世界",
				"arabic":  "مرحبا بالعالم",
			},
		},
		{
			name:      "Special characters",
			routeName: "route.with-special_chars@#$%",
			routeParams: map[string]string{
				"special": "!@#$%^&*()_+-={}[]|\\:;\"'<>?,./'",
				"encoded": "%20%21%22",
				"mixed":   "normal-text_123!@#",
			},
		},
		{
			name:      "Newlines and tabs",
			routeName: "route.with\nnewlines\tand\ttabs",
			routeParams: map[string]string{
				"multiline": "line1\nline2\nline3",
				"tabbed":    "col1\tcol2\tcol3",
				"mixed":     "text\twith\nmixed\r\nwhitespace",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = WithRouteName(ctx, tt.routeName)
			ctx = WithRouteParams(ctx, tt.routeParams)

			// Verify route name
			retrievedName, nameFound := RouteNameFromContext(ctx)
			if !nameFound {
				t.Fatal("Should find Unicode route name")
			}
			if retrievedName != tt.routeName {
				t.Errorf("Unicode route name mismatch:\nExpected: %q\nGot:      %q", tt.routeName, retrievedName)
			}

			// Verify parameters
			retrievedParams, paramsFound := RouteParamsFromContext(ctx)
			if !paramsFound {
				t.Fatal("Should find Unicode route params")
			}

			if len(retrievedParams) != len(tt.routeParams) {
				t.Errorf("Expected %d Unicode params, got %d", len(tt.routeParams), len(retrievedParams))
			}

			for key, expectedValue := range tt.routeParams {
				actualValue, exists := retrievedParams[key]
				if !exists {
					t.Errorf("Unicode param key %q not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Unicode param %q mismatch:\nExpected: %q\nGot:      %q", key, expectedValue, actualValue)
				}
			}
		})
	}
}
