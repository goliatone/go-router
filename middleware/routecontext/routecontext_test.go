package routecontext_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/middleware/routecontext"
)

func TestRouteContextMiddleware_ExportAsMap_Default(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	// Apply middleware with default config (ExportAsMap: true)
	r.Use(routecontext.New())

	// Handler to check exported data
	r.Get("/test/:id", func(ctx router.Context) error {
		templateContext := ctx.Locals("template_context")
		if templateContext == nil {
			t.Error("Expected template_context to be set")
			return ctx.JSON(500, map[string]string{"error": "template_context not set"})
		}

		contextData, ok := templateContext.(map[string]any)
		if !ok {
			t.Error("Expected template_context to be a map")
			return ctx.JSON(500, map[string]string{"error": "template_context not a map"})
		}

		return ctx.JSON(200, contextData)
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/test/123?query=value", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check that route context data is exported as a map
	if _, ok := data["current_route_name"]; !ok {
		t.Error("Expected current_route_name key in exported data")
	}

	if params, ok := data["current_params"]; !ok || params == nil {
		t.Error("Expected current_params in exported data")
	}

	if query, ok := data["current_query"]; !ok || query == nil {
		t.Error("Expected current_query in exported data")
	}

	// Verify the params contain the route parameter
	if paramsMap, ok := data["current_params"].(map[string]any); ok {
		if id, ok := paramsMap["id"]; !ok || id != "123" {
			t.Error("Expected route parameter 'id' to be '123'")
		}
	}

	// Verify the query contains the query parameter
	if queryMap, ok := data["current_query"].(map[string]any); ok {
		if query, ok := queryMap["query"]; !ok || query != "value" {
			t.Error("Expected query parameter 'query' to be 'value'")
		}
	}
}

func TestRouteContextMiddleware_ExportAsMap_True(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	// Apply middleware with ExportAsMap explicitly set to true
	r.Use(routecontext.New(routecontext.Config{
		ExportAsMap: true,
	}))

	// Handler to check exported data
	r.Get("/test/:id", func(ctx router.Context) error {
		templateContext := ctx.Locals("template_context")
		if templateContext == nil {
			t.Error("Expected template_context to be set")
			return ctx.JSON(500, map[string]string{"error": "template_context not set"})
		}

		contextData, ok := templateContext.(map[string]any)
		if !ok {
			t.Error("Expected template_context to be a map")
			return ctx.JSON(500, map[string]string{"error": "template_context not a map"})
		}

		return ctx.JSON(200, contextData)
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/test/123?query=value", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check that route context data is exported as a map
	if _, ok := data["current_route_name"]; !ok {
		t.Error("Expected current_route_name key in exported data")
	}
}

func TestRouteContextMiddleware_ExportAsMap_False(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	// Apply middleware with ExportAsMap set to false (individual keys)
	r.Use(routecontext.New(routecontext.Config{
		ExportAsMap: false,
	}))

	// Handler to check exported data
	r.Get("/test/:id", func(ctx router.Context) error {
		// Check individual keys
		routeName := ctx.Locals("current_route_name")
		params := ctx.Locals("current_params")
		query := ctx.Locals("current_query")

		// Check that template_context is NOT set when ExportAsMap is false
		templateContext := ctx.Locals("template_context")

		return ctx.JSON(200, map[string]any{
			"route_name":       routeName,
			"params":           params,
			"query":            query,
			"template_context": templateContext,
		})
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/test/123?query=value", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check that individual keys are set
	if _, ok := data["route_name"]; !ok {
		t.Error("Expected route_name to be set as individual key")
	}

	if params := data["params"]; params == nil {
		t.Error("Expected params to be set as individual key")
	}

	if query := data["query"]; query == nil {
		t.Error("Expected query to be set as individual key")
	}

	// Check that template_context is NOT set when ExportAsMap is false
	if templateContext := data["template_context"]; templateContext != nil {
		t.Error("Expected template_context to NOT be set when ExportAsMap is false")
	}
}

func TestRouteContextMiddleware_CustomKeys(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	// Apply middleware with custom keys and ExportAsMap false
	r.Use(routecontext.New(routecontext.Config{
		CurrentRouteNameKey: "custom_route",
		CurrentParamsKey:    "custom_params",
		CurrentQueryKey:     "custom_query",
		ExportAsMap:         false,
	}))

	// Handler to check exported data
	r.Get("/test/:id", func(ctx router.Context) error {
		routeName := ctx.Locals("custom_route")
		params := ctx.Locals("custom_params")
		query := ctx.Locals("custom_query")

		return ctx.JSON(200, map[string]any{
			"route_name": routeName,
			"params":     params,
			"query":      query,
		})
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/test/123?query=value", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check that custom keys work
	if _, ok := data["route_name"]; !ok {
		t.Error("Expected custom route name key to work")
	}

	if params := data["params"]; params == nil {
		t.Error("Expected custom params key to work")
	}

	if query := data["query"]; query == nil {
		t.Error("Expected custom query key to work")
	}
}

func TestRouteContextMiddleware_Skip(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	// Apply middleware with skip function
	r.Use(routecontext.New(routecontext.Config{
		Skip: func(c router.Context) bool {
			return c.Path() == "/skip"
		},
		ExportAsMap: false,
	}))

	// Handler that would normally get route context data
	r.Get("/skip", func(ctx router.Context) error {
		routeName := ctx.Locals("current_route_name")
		if routeName != nil {
			t.Error("Expected route context data to be nil when skipped")
		}
		return ctx.JSON(200, map[string]string{"status": "skipped"})
	})

	r.Get("/normal", func(ctx router.Context) error {
		routeName := ctx.Locals("current_route_name")
		if routeName == nil {
			t.Error("Expected route context data to be set when not skipped")
		}
		return ctx.JSON(200, map[string]string{"status": "normal"})
	})

	app := adapter.WrappedRouter()

	// Test skipped path
	req := httptest.NewRequest("GET", "/skip", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}

	// Test normal path
	req = httptest.NewRequest("GET", "/normal", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}
}
