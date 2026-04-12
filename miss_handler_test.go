package router_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	router "github.com/goliatone/go-router"
	"github.com/julienschmidt/httprouter"
)

func TestHTTPRouterHandleMissPreservesExplicitRoutes(t *testing.T) {
	server := router.NewHTTPServer()
	r := server.Router()
	r.Use(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			c.Set("from_miss_middleware", true)
			return next(c)
		}
	})
	r.Get("/search", func(c router.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"handler": "search"})
	})

	registrar, ok := r.(router.MissHandlerRegistrar)
	if !ok {
		t.Fatalf("expected HTTPRouter to support miss handlers")
	}
	registrar.HandleMiss(router.GET, func(c router.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"handler":              "site",
			"from_miss_middleware": c.GetBool("from_miss_middleware", false),
		})
	})

	rec := performHTTPRouterRequest(t, server, http.MethodGet, "/search")
	assertMissPayload(t, rec, http.StatusOK, "search", false)

	rec = performHTTPRouterRequest(t, server, http.MethodGet, "/posts/welcome")
	assertMissPayload(t, rec, http.StatusOK, "site", true)
}

func TestFiberHandleMissPreservesExplicitRoutes(t *testing.T) {
	server := router.NewFiberAdapterWithConfig(router.FiberAdapterConfig{
		PathConflictMode: router.PathConflictModePreferStatic,
		StrictRoutes:     true,
	})
	r := server.Router()
	r.Use(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			c.Set("from_miss_middleware", true)
			return next(c)
		}
	})
	r.Get("/search", func(c router.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"handler": "search"})
	})

	registrar, ok := r.(router.MissHandlerRegistrar)
	if !ok {
		t.Fatalf("expected FiberRouter to support miss handlers")
	}
	registrar.HandleMiss(router.GET, func(c router.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"handler":              "site",
			"from_miss_middleware": c.GetBool("from_miss_middleware", false),
		})
	})

	rec := performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/search")
	assertMissPayload(t, rec, http.StatusOK, "search", false)

	rec = performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/posts/welcome")
	assertMissPayload(t, rec, http.StatusOK, "site", true)
}

func performHTTPRouterRequest(t *testing.T, server router.Server[*httprouter.Router], method, path string) *httptest.ResponseRecorder {
	t.Helper()
	server.Init()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	server.WrappedRouter().ServeHTTP(rec, req)
	return rec
}

func performFiberRequest(t *testing.T, app *fiber.App, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s read body failed: %v", method, path, err)
	}

	rec := httptest.NewRecorder()
	for key, values := range resp.Header {
		for _, value := range values {
			rec.Header().Add(key, value)
		}
	}
	rec.WriteHeader(resp.StatusCode)
	_, _ = rec.Write(body)
	return rec
}

func assertMissPayload(t *testing.T, rec *httptest.ResponseRecorder, status int, wantHandler string, wantMiddleware bool) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, rec.Code, rec.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}

	if got := payload["handler"]; got != wantHandler {
		t.Fatalf("expected handler %q, got %+v", wantHandler, payload)
	}

	gotMiddleware, _ := payload["from_miss_middleware"].(bool)
	if gotMiddleware != wantMiddleware {
		t.Fatalf("expected from_miss_middleware=%v, got %+v", wantMiddleware, payload)
	}
}
