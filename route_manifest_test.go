package router

import (
	"reflect"
	"testing"
)

func TestBuildRouteManifestSortsDeterministically(t *testing.T) {
	routes := []RouteDefinition{
		{Method: POST, Path: "/users", Name: "users.create"},
		{Method: GET, Path: "/health", Name: "health.check"},
		{Method: GET, Path: "/users", Name: "users.list"},
	}

	manifest := BuildRouteManifest(routes)

	expected := []RouteManifestEntry{
		{Method: GET, Path: "/health", Name: "health.check"},
		{Method: GET, Path: "/users", Name: "users.list"},
		{Method: POST, Path: "/users", Name: "users.create"},
	}

	if !reflect.DeepEqual(expected, manifest) {
		t.Fatalf("unexpected manifest order: %#v", manifest)
	}
}

func TestBuildRouterManifestUsesRouterRoutes(t *testing.T) {
	adapter := NewHTTPServer()
	r := adapter.Router()
	r.Get("/users", func(ctx Context) error { return ctx.SendString("ok") }).SetName("users.list")
	r.Post("/users", func(ctx Context) error { return ctx.SendString("ok") }).SetName("users.create")

	manifest := BuildRouterManifest(r)
	if len(manifest) != 2 {
		t.Fatalf("expected 2 manifest entries, got %d", len(manifest))
	}
	if manifest[0].Name != "users.list" || manifest[1].Name != "users.create" {
		t.Fatalf("unexpected manifest entries: %#v", manifest)
	}
}

func TestDiffRouteManifestsClassifiesAddedRemovedAndChanged(t *testing.T) {
	before := []RouteManifestEntry{
		{Method: GET, Path: "/health", Name: "health.check"},
		{Method: GET, Path: "/users/:id", Name: "users.show"},
	}
	after := []RouteManifestEntry{
		{Method: GET, Path: "/healthz", Name: "health.check"},
		{Method: GET, Path: "/users/:id", Name: "users.show"},
		{Method: POST, Path: "/users", Name: "users.create"},
	}

	diff := DiffRouteManifests(before, after)

	if len(diff.Changed) != 1 {
		t.Fatalf("expected 1 changed route, got %d", len(diff.Changed))
	}
	if diff.Changed[0].Identity != "health.check" {
		t.Fatalf("expected changed identity health.check, got %q", diff.Changed[0].Identity)
	}
	if len(diff.Added) != 1 || diff.Added[0].Name != "users.create" {
		t.Fatalf("expected users.create in added, got %#v", diff.Added)
	}
	if len(diff.Removed) != 0 {
		t.Fatalf("expected no removed routes, got %#v", diff.Removed)
	}
}

func TestDiffRouteManifestsDuplicateOrUnnamedEntriesFallBackToAddRemove(t *testing.T) {
	before := []RouteManifestEntry{
		{Method: GET, Path: "/legacy", Name: ""},
		{Method: GET, Path: "/users/:id", Name: "users.show"},
		{Method: POST, Path: "/users/:id", Name: "users.show"},
	}
	after := []RouteManifestEntry{
		{Method: GET, Path: "/legacy", Name: ""},
		{Method: GET, Path: "/members/:id", Name: "users.show"},
		{Method: POST, Path: "/people/:id", Name: "users.show"},
	}

	diff := DiffRouteManifests(before, after)

	if len(diff.Changed) != 0 {
		t.Fatalf("expected no changed routes for duplicate or unnamed identities, got %#v", diff.Changed)
	}
	if len(diff.Added) != 2 {
		t.Fatalf("expected 2 added routes, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 2 {
		t.Fatalf("expected 2 removed routes, got %d", len(diff.Removed))
	}
}
