package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

func TestFiberV3CachedHTTPRequestFollowsContextReplacement(t *testing.T) {
	type key string
	s := router.NewFiberV3Adapter()
	s.Router().Use(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			hc, _ := router.AsHTTPContext(c)
			_ = hc.Request() // Populate the cache before entering native middleware.
			if err := next(c); err != nil {
				return err
			}
			if hc.Request().Context().Value(key("phase")) != "unwound" {
				return fmt.Errorf("HTTP request lost native unwind context")
			}
			return nil
		}
	})
	s.Router().Get("/context", func(c router.Context) error {
		hc, _ := router.AsHTTPContext(c)
		for _, ctx := range []context.Context{c.Context(), hc.Request().Context()} {
			if ctx.Value(key("actor")) != c.Header("X-Actor") || ctx.Err() != context.Canceled {
				return fmt.Errorf("cached request lost actor or cancellation")
			}
		}
		return c.SendString("ok")
	}, router.MiddlewareFromFiberV3(func(c fiber3.Ctx) error {
		ctx, cancel := context.WithCancel(context.WithValue(c.Context(), key("actor"), c.Get("X-Actor")))
		cancel()
		c.SetContext(ctx)
		err := c.Next()
		c.SetContext(context.WithValue(c.Context(), key("phase"), "unwound"))
		return err
	}))
	app := s.WrappedRouter()
	var workers sync.WaitGroup
	for i := range 24 {
		workers.Go(func() {
			req := httptest.NewRequest("GET", "/context", nil)
			req.Header.Set("X-Actor", fmt.Sprint(i))
			code, body, err := serveFiberMiddlewareRequestV3(app, req)
			if err != nil || code != 200 || body != "ok" {
				t.Errorf("request %d: %d %q %v", i, code, body, err)
			}
		})
	}
	workers.Wait()
}

func TestFiberV3HTTPBridgeResponseState(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		code               int
		handler            http.HandlerFunc
	}{
		{"denial", "GET", "denied", 401, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401); _, _ = w.Write([]byte("denied")) }},
		{"head", "HEAD", "", 401, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401); _, _ = w.Write([]byte("denied")) }},
		{"empty-write", "GET", "", 200, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte{}) }},
		{"no-content", "GET", "", 204, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }},
		{"no-op", "GET", "", 200, func(http.ResponseWriter, *http.Request) {}},
		{"flush", "GET", "", 200, func(w http.ResponseWriter, _ *http.Request) { w.(http.Flusher).Flush() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := router.NewFiberV3Adapter()
			s.Router().Use(func(next router.HandlerFunc) router.HandlerFunc {
				return func(c router.Context) error {
					if err := next(c); err != nil {
						return err
					}
					state, _ := router.AsResponseState(c)
					if state.StatusCode() != tc.code || !state.ResponseWritten() || state.ResponseBodySize() != int64(len(tc.body)) || state.ResponseIsStream() {
						return fmt.Errorf("HTTP bridge state: %d %t %d %t", state.StatusCode(), state.ResponseWritten(), state.ResponseBodySize(), state.ResponseIsStream())
					}
					return nil
				}
			})
			s.Router().Handle(router.HTTPMethod(tc.method), "/http", router.HandlerFromHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Set-Cookie", "one=1")
				w.Header().Add("Set-Cookie", "two=2")
				tc.handler(w, r)
			})))
			code, body, headers := v3Response(t, s.WrappedRouter(), tc.method, "/http", "")
			if code != tc.code || body != tc.body || len(headers.Values("Set-Cookie")) != 2 {
				t.Fatalf("wire: %d %q %v", code, body, headers)
			}
		})
	}
}

func TestFiberV3DirectHTTPWriterUpdatesState(t *testing.T) {
	s := router.NewFiberV3Adapter()
	s.Router().Get("/direct", func(c router.Context) error {
		hc, _ := router.AsHTTPContext(c)
		w := hc.Response()
		w.WriteHeader(202)
		if _, err := w.Write([]byte("direct")); err != nil {
			return err
		}
		state, _ := router.AsResponseState(c)
		if state.StatusCode() != 202 || !state.ResponseWritten() || state.ResponseBodySize() != 6 {
			return fmt.Errorf("direct HTTP writer state not synchronized")
		}
		return nil
	})
	code, body, _ := v3Response(t, s.WrappedRouter(), "GET", "/direct", "")
	if code != 202 || body != "direct" {
		t.Fatalf("wire: %d %q", code, body)
	}
}
