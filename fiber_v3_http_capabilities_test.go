package router_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

func TestFiberV3HTTPBridgeEmptyHeadAndCookies(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		status             int
		handler            http.HandlerFunc
	}{
		{"denial", "GET", "denied", 401, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401); _, _ = io.WriteString(w, "denied") }},
		{"header-only", "GET", "", 403, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }},
		{"empty-write", "GET", "", 200, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(nil) }},
		{"empty-handler", "GET", "", 200, func(http.ResponseWriter, *http.Request) {}},
		{"no-content", "GET", "", 204, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }},
		{"head", "HEAD", "", 202, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202); _, _ = io.WriteString(w, "discard") }},
		{"implicit-status", "GET", "written", 200, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "written") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := router.NewFiberV3Adapter()
			s.Router().Use(func(next router.HandlerFunc) router.HandlerFunc {
				return func(c router.Context) error {
					if err := next(c); err != nil {
						return err
					}
					state, _ := router.AsResponseState(c)
					if state.StatusCode() != tc.status || !state.ResponseWritten() || state.ResponseBodySize() != int64(len(tc.body)) || state.ResponseIsStream() {
						return fmt.Errorf("HTTP bridge state: code=%d written=%t size=%d stream=%t", state.StatusCode(), state.ResponseWritten(), state.ResponseBodySize(), state.ResponseIsStream())
					}
					if got := state.ResponseHeaders().Values("Set-Cookie"); len(got) != 2 {
						return fmt.Errorf("response cookies: %v", got)
					}
					return nil
				}
			})
			h := router.HandlerFromHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Set-Cookie", "one=1")
				w.Header().Add("Set-Cookie", "two=2")
				tc.handler(w, r)
			}))
			if tc.method == "HEAD" {
				s.Router().Head("/bridge", h)
			} else {
				s.Router().Get("/bridge", h)
			}
			code, body, header := v3Response(t, s.WrappedRouter(), tc.method, "/bridge", "")
			if code != tc.status || body != tc.body || len(header.Values("Set-Cookie")) != 2 {
				t.Fatalf("wire: %d %q cookies=%v", code, body, header.Values("Set-Cookie"))
			}
		})
	}
}

func TestFiberV3HTTPCachedRequestContextReplacement(t *testing.T) {
	type key struct{}
	s := router.NewFiberV3Adapter()
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "alice"))
	defer cancel()
	s.Router().Use(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			hc, _ := router.AsHTTPContext(c)
			old := hc.Request()
			if err := next(c); err != nil {
				return err
			}
			if old.Context().Value(key{}) != nil {
				return fmt.Errorf("replaced context mutated a previously returned request")
			}
			if hc.Request().Context().Value(key{}) != "after" {
				return fmt.Errorf("native unwind context not synchronized")
			}
			c.SetContext(nil)
			if hc.Request().Context().Value(key{}) != nil || hc.Request().Context().Err() != nil {
				return fmt.Errorf("nil context reset did not refresh HTTP request")
			}
			return nil
		}
	})
	s.Router().Get("/context", func(c router.Context) error {
		hc, _ := router.AsHTTPContext(c)
		if hc.Request().Context().Value(key{}) != "alice" {
			return fmt.Errorf("actor was lost from cached request")
		}
		cancel()
		if hc.Request().Context().Err() != context.Canceled {
			return fmt.Errorf("replacement cancellation was lost")
		}
		return c.SendString("ok")
	}, router.MiddlewareFromFiberV3(func(c fiber3.Ctx) error {
		c.SetContext(parent)
		err := c.Next()
		c.SetContext(context.WithValue(context.Background(), key{}, "after"))
		return err
	}))
	code, body, _ := v3Response(t, s.WrappedRouter(), "GET", "/context", "")
	if code != 200 || body != "ok" {
		t.Fatalf("context bridge: %d %q", code, body)
	}
}
