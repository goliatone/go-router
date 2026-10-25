package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

type middlewareContextKeyV3 string

func TestMiddlewareFromFiberPreservesCurrentUserContextV3(t *testing.T) {
	for _, mode := range []string{"before-next", "nested-unwind", "short-circuit", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			srv := router.NewFiberV3Adapter()
			r := srv.Router()
			r.Use(func(next router.HandlerFunc) router.HandlerFunc {
				return func(c router.Context) error {
					// Populate the router context cache before crossing the native boundary.
					c.SetContext(context.WithValue(c.Context(), middlewareContextKeyV3("seed"), "router"))
					err := next(c)
					if got := c.Context().Value(middlewareContextKeyV3("actor")); got != "user-a" {
						return fmt.Errorf("upstream lost native actor: %v", got)
					}
					if mode == "nested-unwind" && c.Context().Value(middlewareContextKeyV3("phase")) != "unwound" {
						return fmt.Errorf("upstream lost native unwind context")
					}
					return err
				}
			})
			r.Use(router.MiddlewareFromFiberV3(func(c fiber.Ctx) error {
				if c.Context().Value(middlewareContextKeyV3("seed")) != "router" {
					return fmt.Errorf("native lost router seed")
				}
				c.SetContext(context.WithValue(c.Context(), middlewareContextKeyV3("actor"), "user-a"))
				if mode == "short-circuit" {
					return c.SendStatus(http.StatusForbidden)
				}
				if mode == "canceled" {
					ctx, cancel := context.WithCancel(c.Context())
					cancel()
					c.SetContext(ctx)
				}
				err := c.Next()
				if mode == "nested-unwind" {
					if c.Context().Value(middlewareContextKeyV3("phase")) != "handler" {
						return fmt.Errorf("native lost handler context")
					}
					c.SetContext(context.WithValue(c.Context(), middlewareContextKeyV3("phase"), "unwound"))
				}
				return err
			}))
			r.Use(router.MiddlewareFromFiberV3(func(c fiber.Ctx) error {
				c.SetContext(context.WithValue(c.Context(), middlewareContextKeyV3("correlation"), "request-a"))
				return c.Next()
			}))
			r.Get("/resource", func(c router.Context) error {
				if c.Context().Value(middlewareContextKeyV3("actor")) != "user-a" || c.Context().Value(middlewareContextKeyV3("correlation")) != "request-a" {
					return fmt.Errorf("handler lost native actor/correlation")
				}
				if mode == "canceled" && c.Context().Err() != context.Canceled {
					return fmt.Errorf("handler lost cancellation")
				}
				c.SetContext(context.WithValue(c.Context(), middlewareContextKeyV3("phase"), "handler"))
				return c.SendStatus(http.StatusOK)
			})
			srv.Init()
			resp, err := srv.WrappedRouter().Test(httptest.NewRequest("GET", "/resource", nil), fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			want := http.StatusOK
			if mode == "short-circuit" {
				want = http.StatusForbidden
			}
			if resp.StatusCode != want {
				t.Fatalf("status %d, want %d", resp.StatusCode, want)
			}
		})
	}
}

func TestMiddlewareFromFiberCurrentUserContextRequestIsolationV3(t *testing.T) {
	srv := router.NewFiberV3Adapter()
	r := srv.Router()
	r.Use(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error { _ = c.Context(); return next(c) }
	})
	r.Use(router.MiddlewareFromFiberV3(func(c fiber.Ctx) error {
		c.SetContext(context.WithValue(c.Context(), middlewareContextKeyV3("actor"), c.Get("X-Fixture-Actor")))
		return c.Next()
	}))
	r.Get("/resource", func(c router.Context) error {
		if c.Context().Value(middlewareContextKeyV3("actor")) != c.Header("X-Fixture-Actor") {
			return fmt.Errorf("request actor mismatch")
		}
		return c.SendStatus(http.StatusOK)
	})
	srv.Init()
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/resource", nil)
			req.Header.Set("X-Fixture-Actor", fmt.Sprintf("user-%d", i))
			resp, err := srv.WrappedRouter().Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("request %d status %d", i, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
}
