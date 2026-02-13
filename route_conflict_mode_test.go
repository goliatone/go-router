package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goliatone/go-router"
)

func TestValidateRouteDefinitionsWithOptions_StaticParamSiblingMode(t *testing.T) {
	routes := []*router.RouteDefinition{
		{Method: router.POST, Path: "/admin/api/v1/users/bulk/:action"},
		{Method: router.POST, Path: "/admin/api/v1/users/bulk/assign-role"},
	}

	strictErrs := router.ValidateRouteDefinitionsWithOptions(routes, router.RouteValidationOptions{
		PathConflictMode: router.PathConflictModeStrict,
	})
	if len(strictErrs) == 0 {
		t.Fatal("expected strict mode to report static/param sibling conflict")
	}

	preferStaticErrs := router.ValidateRouteDefinitionsWithOptions(routes, router.RouteValidationOptions{
		PathConflictMode: router.PathConflictModePreferStatic,
	})
	if len(preferStaticErrs) != 0 {
		t.Fatalf("expected prefer_static mode to allow static/param siblings, got %d errors", len(preferStaticErrs))
	}
}

func TestValidateRouteDefinitionsWithOptions_SharedParamPrefixStaticSibling(t *testing.T) {
	routes := []*router.RouteDefinition{
		{Method: router.GET, Path: "/admin/content/:name/:entry"},
		{Method: router.GET, Path: "/admin/content/:name/new"},
	}

	strictErrs := router.ValidateRouteDefinitionsWithOptions(routes, router.RouteValidationOptions{
		PathConflictMode: router.PathConflictModeStrict,
	})
	if len(strictErrs) == 0 {
		t.Fatal("expected strict mode to report conflict for shared param prefix with static sibling")
	}
	if !strings.Contains(strictErrs[0].Error(), "static segment conflicts with wildcard segment") {
		t.Fatalf("expected strict conflict at static-vs-param segment, got %q", strictErrs[0].Error())
	}

	preferStaticErrs := router.ValidateRouteDefinitionsWithOptions(routes, router.RouteValidationOptions{
		PathConflictMode: router.PathConflictModePreferStatic,
	})
	if len(preferStaticErrs) != 0 {
		t.Fatalf("expected prefer_static mode to allow later static-vs-param sibling, got %d errors", len(preferStaticErrs))
	}
}

func TestValidateRouteDefinitionsWithOptions_DuplicateStillConflicts(t *testing.T) {
	routes := []*router.RouteDefinition{
		{Method: router.GET, Path: "/users/:id"},
		{Method: router.GET, Path: "/users/:id"},
	}

	errs := router.ValidateRouteDefinitionsWithOptions(routes, router.RouteValidationOptions{
		PathConflictMode: router.PathConflictModePreferStatic,
	})
	if len(errs) == 0 {
		t.Fatal("expected duplicate route conflict even in prefer_static mode")
	}
}

func TestValidateRouteDefinitionsWithOptions_CatchAllConflictsDisabledByDefault(t *testing.T) {
	routes := []*router.RouteDefinition{
		{Method: router.GET, Path: "/files/*filepath"},
		{Method: router.GET, Path: "/files/:id"},
	}

	errs := router.ValidateRouteDefinitionsWithOptions(routes, router.RouteValidationOptions{
		PathConflictMode: router.PathConflictModePreferStatic,
	})
	if len(errs) != 0 {
		t.Fatalf("expected catch-all conflicts to be disabled by default, got %d errors", len(errs))
	}
}

func TestValidateRouteDefinitionsWithOptions_CatchAllConflictsCanBeEnforced(t *testing.T) {
	routes := []*router.RouteDefinition{
		{Method: router.GET, Path: "/files/*filepath"},
		{Method: router.GET, Path: "/files/:id"},
	}

	errs := router.ValidateRouteDefinitionsWithOptions(routes, router.RouteValidationOptions{
		PathConflictMode:         router.PathConflictModePreferStatic,
		EnforceCatchAllConflicts: true,
	})
	if len(errs) == 0 {
		t.Fatal("expected catch-all conflict when enforcement is enabled")
	}
}

func TestFiberPreferStaticMode_DeterministicDispatch(t *testing.T) {
	tests := []struct {
		name        string
		staticFirst bool
	}{
		{
			name:        "param first",
			staticFirst: false,
		},
		{
			name:        "static first",
			staticFirst: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := router.NewFiberAdapterWithConfig(router.FiberAdapterConfig{
				PathConflictMode: router.PathConflictModePreferStatic,
				StrictRoutes:     true,
			})

			r := adapter.Router()
			staticHandler := func(ctx router.Context) error {
				return ctx.SendString("static")
			}
			paramHandler := func(ctx router.Context) error {
				return ctx.SendString("param:" + ctx.Param("action"))
			}

			if tt.staticFirst {
				r.Post("/admin/api/v1/users/bulk/assign-role", staticHandler)
				r.Post("/admin/api/v1/users/bulk/:action", paramHandler)
			} else {
				r.Post("/admin/api/v1/users/bulk/:action", paramHandler)
				r.Post("/admin/api/v1/users/bulk/assign-role", staticHandler)
			}

			app := adapter.WrappedRouter()

			reqStatic := httptest.NewRequest(http.MethodPost, "/admin/api/v1/users/bulk/assign-role", nil)
			respStatic, err := app.Test(reqStatic)
			if err != nil {
				t.Fatalf("static request failed: %v", err)
			}
			staticBody, err := io.ReadAll(respStatic.Body)
			respStatic.Body.Close()
			if err != nil {
				t.Fatalf("reading static response failed: %v", err)
			}
			if got := string(staticBody); got != "static" {
				t.Fatalf("expected static route to win, got %q", got)
			}

			reqParam := httptest.NewRequest(http.MethodPost, "/admin/api/v1/users/bulk/suspend", nil)
			respParam, err := app.Test(reqParam)
			if err != nil {
				t.Fatalf("param request failed: %v", err)
			}
			paramBody, err := io.ReadAll(respParam.Body)
			respParam.Body.Close()
			if err != nil {
				t.Fatalf("reading param response failed: %v", err)
			}
			if got := string(paramBody); got != "param:suspend" {
				t.Fatalf("expected param route for non-static value, got %q", got)
			}
		})
	}
}

func TestFiberStrictMode_StaticParamSiblingStillConflicts(t *testing.T) {
	adapter := router.NewFiberAdapterWithConfig(router.FiberAdapterConfig{
		StrictRoutes: true,
	})
	r := adapter.Router()

	handler := func(ctx router.Context) error { return ctx.SendString("ok") }
	r.Post("/admin/api/v1/users/bulk/assign-role", handler)
	r.Post("/admin/api/v1/users/bulk/:action", handler)

	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("expected strict mode to panic on static/param sibling conflict")
		}
	}()

	adapter.Init()
}

func TestFiberPreferStaticMode_CatchAllDispatchDeterministic(t *testing.T) {
	tests := []struct {
		name          string
		catchAllFirst bool
	}{
		{name: "catch-all first", catchAllFirst: true},
		{name: "catch-all last", catchAllFirst: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := router.NewFiberAdapterWithConfig(router.FiberAdapterConfig{
				PathConflictMode: router.PathConflictModePreferStatic,
				StrictRoutes:     true,
			})
			r := adapter.Router()

			catchAllHandler := func(ctx router.Context) error { return ctx.SendString("catch-all") }
			postsIndexHandler := func(ctx router.Context) error { return ctx.SendString("posts-index") }
			postDetailHandler := func(ctx router.Context) error {
				return ctx.SendString("post-detail:" + ctx.Param("slug"))
			}

			if tt.catchAllFirst {
				r.Get("/*", catchAllHandler)
			}

			r.Get("/posts", postsIndexHandler)
			r.Get("/posts/:slug", postDetailHandler)

			if !tt.catchAllFirst {
				r.Get("/*", catchAllHandler)
			}

			app := adapter.WrappedRouter()

			indexResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/posts", nil), -1)
			if err != nil {
				t.Fatalf("posts index request failed: %v", err)
			}
			indexBody, err := io.ReadAll(indexResp.Body)
			indexResp.Body.Close()
			if err != nil {
				t.Fatalf("reading posts index response failed: %v", err)
			}
			if got := string(indexBody); got != "posts-index" {
				t.Fatalf("expected /posts to resolve to posts index, got %q", got)
			}

			detailResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/posts/hello", nil), -1)
			if err != nil {
				t.Fatalf("post detail request failed: %v", err)
			}
			detailBody, err := io.ReadAll(detailResp.Body)
			detailResp.Body.Close()
			if err != nil {
				t.Fatalf("reading post detail response failed: %v", err)
			}
			if got := string(detailBody); got != "post-detail:hello" {
				t.Fatalf("expected /posts/:slug to resolve to post detail, got %q", got)
			}

			fallbackResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/admin/logout", nil), -1)
			if err != nil {
				t.Fatalf("fallback request failed: %v", err)
			}
			fallbackBody, err := io.ReadAll(fallbackResp.Body)
			fallbackResp.Body.Close()
			if err != nil {
				t.Fatalf("reading fallback response failed: %v", err)
			}
			if got := string(fallbackBody); got != "catch-all" {
				t.Fatalf("expected fallback path to resolve to catch-all, got %q", got)
			}
		})
	}
}

func TestHTTPRouterPreferStaticModeUnsupported(t *testing.T) {
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic for unsupported prefer_static mode on HTTPRouter")
		}
		msg := rec.(error).Error()
		if !strings.Contains(msg, "not supported") {
			t.Fatalf("expected unsupported-mode error message, got: %s", msg)
		}
	}()

	_ = router.NewHTTPServer(
		router.WithHTTPRouterPathConflictMode(router.PathConflictModePreferStatic),
	)
}

func TestHTTPRouterStrictRoutes_StaticParamSiblingStillConflicts(t *testing.T) {
	adapter := router.NewHTTPServer(
		router.WithHTTPServerStrictRoutes(true),
	)
	r := adapter.Router()

	handler := func(ctx router.Context) error { return ctx.SendString("ok") }
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("expected strict-mode conflict panic on HTTPRouter static/param sibling conflict")
		}
	}()

	r.Post("/admin/api/v1/users/bulk/assign-role", handler)
	r.Post("/admin/api/v1/users/bulk/:action", handler)
	adapter.Init()
}
