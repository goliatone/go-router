package router

import (
	"os"
	"testing"
)

func TestFiberStrictInitAllowsInternalStaticRouteNames(t *testing.T) {
	assetDir := t.TempDir()
	if err := os.WriteFile(assetDir+"/index.html", []byte("ok"), 0o644); err != nil {
		t.Fatalf("failed to write static asset: %v", err)
	}

	adapter := NewFiberAdapterWithConfig(FiberAdapterConfig{
		StrictRoutes:     true,
		NamedRoutePolicy: NamedRouteCollisionPolicyError,
	})
	adapter.Router().Static("/public", assetDir)

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("expected strict init to ignore internal static route names, got panic %v", rec)
		}
	}()

	adapter.Init()
}

func TestHTTPStrictInitAllowsInternalStaticRouteNames(t *testing.T) {
	assetDir := t.TempDir()
	if err := os.WriteFile(assetDir+"/index.html", []byte("ok"), 0o644); err != nil {
		t.Fatalf("failed to write static asset: %v", err)
	}

	adapter := NewHTTPServer(
		WithHTTPServerStrictRoutes(true),
		WithHTTPRouterNamedRoutePolicy(NamedRouteCollisionPolicyError),
	)
	adapter.Router().Static("/public", assetDir)

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("expected strict init to ignore internal static route names, got panic %v", rec)
		}
	}()

	adapter.Init()
}

func TestHTTPStrictInitAllowsMultipleUnnamedWebSocketRoutes(t *testing.T) {
	adapter := NewHTTPServer(
		WithHTTPServerStrictRoutes(true),
		WithHTTPRouterNamedRoutePolicy(NamedRouteCollisionPolicyError),
	)
	r := adapter.Router()

	r.WebSocket("/ws/notifications", WebSocketConfig{}, func(WebSocketContext) error { return nil })
	r.WebSocket("/ws/activity", WebSocketConfig{}, func(WebSocketContext) error { return nil })

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("expected strict init to ignore internal websocket route names, got panic %v", rec)
		}
	}()

	adapter.Init()
}

func TestServeOpenAPIInternalRoutesDoNotConsumePublicLookupNamespace(t *testing.T) {
	adapter := NewHTTPServer(
		WithHTTPServerStrictRoutes(true),
		WithHTTPRouterNamedRoutePolicy(NamedRouteCollisionPolicyError),
	)
	r := adapter.Router().(*HTTPRouter)

	r.Get("/docs", func(ctx Context) error { return ctx.SendString("ok") }).SetName("openapi.json")
	ServeOpenAPI(r, NewOpenAPIRenderer())

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("expected strict init to ignore internal OpenAPI helper names, got panic %v", rec)
		}
	}()

	adapter.Init()

	got := r.GetRoute("openapi.json")
	if got == nil || got.Path != "/docs" {
		t.Fatalf("expected public lookup to resolve explicit route, got %+v", got)
	}

	name, ok := r.RouteNameFromPath("GET", "/openapi.json")
	if !ok || name != "openapi.json" {
		t.Fatalf("expected helper route to retain internal runtime name, got %q, %v", name, ok)
	}

	manifest := BuildRouterManifest(r)
	for _, entry := range manifest {
		if entry.Path == "/openapi.json" && entry.Name != "" {
			t.Fatalf("expected internal helper route to have blank public manifest name, got %#v", entry)
		}
	}
}

func TestFiberStrictInitRejectsExplicitlyNamedWebSocketConflicts(t *testing.T) {
	adapter := NewFiberAdapterWithConfig(FiberAdapterConfig{
		StrictRoutes:     true,
		NamedRoutePolicy: NamedRouteCollisionPolicyError,
	})
	r := adapter.Router()

	r.WebSocket("/ws/notifications", WebSocketConfig{}, func(WebSocketContext) error { return nil }).SetName("ws.notifications")
	r.WebSocket("/ws/activity", WebSocketConfig{}, func(WebSocketContext) error { return nil }).SetName("ws.notifications")

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected strict init to panic for explicit websocket name conflict")
		}
	}()

	adapter.Init()
}
