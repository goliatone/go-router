package router

import (
	"fmt"
	"strings"
	"testing"
)

func TestFiberStrictInitPanicsOnNamedRouteConflictWhenPolicyIsError(t *testing.T) {
	adapter := NewFiberAdapterWithConfig(FiberAdapterConfig{
		StrictRoutes:     true,
		NamedRoutePolicy: NamedRouteCollisionPolicyError,
	})
	r := adapter.Router()

	r.Get("/users/:id", func(ctx Context) error { return ctx.SendString("ok") }).SetName("users.show")
	r.Get("/members/:id", func(ctx Context) error { return ctx.SendString("ok") }).SetName("users.show")

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected strict init to panic")
		}
		if !strings.Contains(fmt.Sprint(rec), "ROUTE_NAME_CONFLICT") {
			t.Fatalf("expected ROUTE_NAME_CONFLICT in panic, got %v", rec)
		}
	}()

	adapter.Init()
}

func TestHTTPStrictInitPanicsOnNamedRouteConflictWhenPolicyIsError(t *testing.T) {
	adapter := NewHTTPServer(
		WithHTTPServerStrictRoutes(true),
		WithHTTPRouterNamedRoutePolicy(NamedRouteCollisionPolicyError),
	)
	r := adapter.Router()

	r.Get("/users/:id", func(ctx Context) error { return ctx.SendString("ok") }).SetName("users.show")
	r.Get("/members/:id", func(ctx Context) error { return ctx.SendString("ok") }).SetName("users.show")

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected strict init to panic")
		}
		if !strings.Contains(fmt.Sprint(rec), "ROUTE_NAME_CONFLICT") {
			t.Fatalf("expected ROUTE_NAME_CONFLICT in panic, got %v", rec)
		}
	}()

	adapter.Init()
}

func TestHTTPNamedRouteDefaultPolicyRemainsBackwardCompatible(t *testing.T) {
	adapter := NewHTTPServer()
	r := adapter.Router().(*HTTPRouter)

	r.Get("/users/:id", func(ctx Context) error { return ctx.SendString("ok") }).SetName("users.show")
	r.Get("/members/:id", func(ctx Context) error { return ctx.SendString("ok") }).SetName("users.show")

	adapter.Init()

	got := r.GetRoute("users.show")
	if got == nil {
		t.Fatal("expected named route lookup to resolve")
	}
	if got.Path != "/members/:id" {
		t.Fatalf("expected latest route to win by default, got %q", got.Path)
	}
}
