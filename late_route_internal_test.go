package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func denyLateRouteMiddleware(HandlerFunc) HandlerFunc {
	return func(c Context) error {
		return c.Status(http.StatusForbidden).SendString("forbidden")
	}
}

func lateRouteOK(c Context) error {
	return c.SendString("ok")
}

func TestHTTPLateRouteKeepsDeclaringGroupMiddleware(t *testing.T) {
	server := NewHTTPServer()
	group, ok := server.Router().Group("/private").(*HTTPRouter)
	if !ok {
		t.Fatalf("expected *HTTPRouter group")
	}
	group.Use(denyLateRouteMiddleware)
	group.addLateRoute(GET, "/private/late", lateRouteOK, "private.late")

	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private/late", nil)
	server.WrappedRouter().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body %q)", rr.Code, http.StatusForbidden, rr.Body.String())
	}
}

func TestFiberLateRouteKeepsDeclaringGroupMiddleware(t *testing.T) {
	server := NewFiberAdapter()
	group, ok := server.Router().Group("/private").(*FiberRouter)
	if !ok {
		t.Fatalf("expected *FiberRouter group")
	}
	group.Use(denyLateRouteMiddleware)
	group.addLateRoute(GET, "/private/late", lateRouteOK, "private.late")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private/late", nil)
	resp, err := server.WrappedRouter().Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("failed to close body: %v", closeErr)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusForbidden, body)
	}
}
