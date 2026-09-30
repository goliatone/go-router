package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
	"github.com/julienschmidt/httprouter"
)

const (
	staticTraceHeader = "X-Static-Trace"
	staticFixtureCSS  = "body { color: red; }"
)

type staticMiddlewareRequest struct {
	method string
	path   string
	status int
	body   string // compared when not empty
	trace  string // expected staticTraceHeader value
}

type staticMiddlewareResponse struct {
	status int
	body   string
	trace  string
}

func denyStaticMiddleware(router.HandlerFunc) router.HandlerFunc {
	return func(c router.Context) error {
		return c.Status(http.StatusForbidden).SendString("forbidden")
	}
}

// traceStaticMiddleware appends name to the trace header so tests can assert
// which middleware ran, in what order, and how many times.
func traceStaticMiddleware(name string) router.MiddlewareFunc {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			trace := name
			if prev := c.GetString("static_trace", ""); prev != "" {
				trace = prev + "," + name
			}
			c.Set("static_trace", trace)
			c.SetHeader(staticTraceHeader, trace)
			return next(c)
		}
	}
}

func TestStatic_HTTP_AppliesRouterMiddleware(t *testing.T) {
	testStaticAppliesRouterMiddleware(t, func() router.Server[*httprouter.Router] {
		return router.NewHTTPServer()
	}, serveHTTPStatic)
}

func TestStatic_Fiber_AppliesRouterMiddleware(t *testing.T) {
	testStaticAppliesRouterMiddleware(t, func() router.Server[*fiber.App] {
		return router.NewFiberAdapter()
	}, serveFiberStatic)
}

func testStaticAppliesRouterMiddleware[T any](
	t *testing.T,
	newServer func() router.Server[T],
	serve func(*testing.T, router.Server[T], string, string) staticMiddlewareResponse,
) {
	t.Helper()
	dir := writeStaticFixture(t)

	tests := []struct {
		name     string
		setup    func(r router.Router[T])
		requests []staticMiddlewareRequest
	}{
		{
			name: "group use guards group static",
			setup: func(r router.Router[T]) {
				private := r.Group("/private")
				private.Use(denyStaticMiddleware)
				private.Static("/files", dir)
				r.Group("/public").Static("/files", dir)
			},
			requests: []staticMiddlewareRequest{
				{method: http.MethodGet, path: "/private/files/style.css", status: http.StatusForbidden, body: "forbidden"},
				{method: http.MethodGet, path: "/private/files", status: http.StatusForbidden, body: "forbidden"},
				{method: http.MethodHead, path: "/private/files/style.css", status: http.StatusForbidden},
				{method: http.MethodGet, path: "/public/files/style.css", status: http.StatusOK, body: staticFixtureCSS},
			},
		},
		{
			name: "nested group inherits parent use",
			setup: func(r router.Router[T]) {
				outer := r.Group("/outer")
				outer.Use(denyStaticMiddleware)
				outer.Group("/inner").Static("/files", dir)
			},
			requests: []staticMiddlewareRequest{
				{method: http.MethodGet, path: "/outer/inner/files/style.css", status: http.StatusForbidden, body: "forbidden"},
				{method: http.MethodHead, path: "/outer/inner/files/style.css", status: http.StatusForbidden},
			},
		},
		{
			name: "nested group use does not leak to parent",
			setup: func(r router.Router[T]) {
				outer := r.Group("/outer")
				inner := outer.Group("/inner")
				inner.Use(denyStaticMiddleware)
				inner.Static("/files", dir)
				outer.Static("/files", dir)
			},
			requests: []staticMiddlewareRequest{
				{method: http.MethodGet, path: "/outer/inner/files/style.css", status: http.StatusForbidden, body: "forbidden"},
				{method: http.MethodGet, path: "/outer/files/style.css", status: http.StatusOK, body: staticFixtureCSS},
			},
		},
		{
			name: "nested group middleware runs once in declaration order",
			setup: func(r router.Router[T]) {
				r.Use(traceStaticMiddleware("root"))
				outer := r.Group("/outer")
				outer.Use(traceStaticMiddleware("outer"))
				inner := outer.Group("/inner")
				inner.Use(traceStaticMiddleware("inner"))
				inner.Static("/files", dir)
			},
			requests: []staticMiddlewareRequest{
				{method: http.MethodGet, path: "/outer/inner/files/style.css", status: http.StatusOK, body: staticFixtureCSS, trace: "root,outer,inner"},
				{method: http.MethodHead, path: "/outer/inner/files/style.css", status: http.StatusOK, trace: "root,outer,inner"},
			},
		},
		{
			name: "root use guards root and group static",
			setup: func(r router.Router[T]) {
				r.Use(denyStaticMiddleware)
				r.Static("/files", dir)
				r.Group("/api").Static("/files", dir)
			},
			requests: []staticMiddlewareRequest{
				{method: http.MethodGet, path: "/files/style.css", status: http.StatusForbidden, body: "forbidden"},
				{method: http.MethodGet, path: "/files", status: http.StatusForbidden, body: "forbidden"},
				{method: http.MethodHead, path: "/files/style.css", status: http.StatusForbidden},
				{method: http.MethodGet, path: "/api/files/style.css", status: http.StatusForbidden, body: "forbidden"},
			},
		},
		{
			name: "root use guards root prefix static",
			setup: func(r router.Router[T]) {
				r.Use(denyStaticMiddleware)
				r.Static("/", dir)
			},
			requests: []staticMiddlewareRequest{
				{method: http.MethodGet, path: "/style.css", status: http.StatusForbidden, body: "forbidden"},
				{method: http.MethodGet, path: "/", status: http.StatusForbidden, body: "forbidden"},
				{method: http.MethodHead, path: "/style.css", status: http.StatusForbidden},
			},
		},
		{
			// Static captures middleware when declared, like any other route.
			name: "use after static matches ordinary route semantics",
			setup: func(r router.Router[T]) {
				r.Use(traceStaticMiddleware("before"))
				r.Get("/ping", func(c router.Context) error {
					return c.SendString("pong")
				})
				r.Static("/files", dir)
				r.Use(traceStaticMiddleware("after"))
			},
			requests: []staticMiddlewareRequest{
				{method: http.MethodGet, path: "/ping", status: http.StatusOK, body: "pong", trace: "before"},
				{method: http.MethodGet, path: "/files/style.css", status: http.StatusOK, body: staticFixtureCSS, trace: "before"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newServer()
			tt.setup(server.Router())

			for _, req := range tt.requests {
				got := serve(t, server, req.method, req.path)
				if got.status != req.status {
					t.Errorf("%s %s status = %d, want %d (body %q)", req.method, req.path, got.status, req.status, got.body)
				}
				if req.body != "" && got.body != req.body {
					t.Errorf("%s %s body = %q, want %q", req.method, req.path, got.body, req.body)
				}
				if got.trace != req.trace {
					t.Errorf("%s %s trace = %q, want %q", req.method, req.path, got.trace, req.trace)
				}
			}
		})
	}
}

func serveHTTPStatic(t *testing.T, server router.Server[*httprouter.Router], method, path string) staticMiddlewareResponse {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	server.WrappedRouter().ServeHTTP(rr, req)
	return staticMiddlewareResponse{
		status: rr.Code,
		body:   rr.Body.String(),
		trace:  rr.Header().Get(staticTraceHeader),
	}
}

func serveFiberStatic(t *testing.T, server router.Server[*fiber.App], method, path string) staticMiddlewareResponse {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	resp, err := server.WrappedRouter().Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s read body failed: %v", method, path, err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("%s %s close body failed: %v", method, path, closeErr)
	}
	return staticMiddlewareResponse{
		status: resp.StatusCode,
		body:   string(body),
		trace:  resp.Header.Get(staticTraceHeader),
	}
}
