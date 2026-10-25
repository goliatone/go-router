package router_test

import (
	"fmt"
	"strings"
	"testing"

	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

func TestFiberV3NativeMiddlewareResponseState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mw      fiber3.Handler
		code    int
		body    string
		written bool
		stream  bool
	}{
		{"denial", func(c fiber3.Ctx) error { return c.Status(401).SendString("denied") }, 401, "denied", true, false},
		{"empty", func(c fiber3.Ctx) error { return c.Send([]byte{}) }, 200, "", true, false},
		{"no-content", func(c fiber3.Ctx) error { return c.SendStatus(204) }, 204, "", true, false},
		{"pass-through", func(c fiber3.Ctx) error { return c.Next() }, 200, "", false, false},
		{"unwind", func(c fiber3.Ctx) error {
			if err := c.Next(); err != nil {
				return err
			}
			return c.Status(202).SendString("replaced")
		}, 202, "replaced", true, false},
		{"stream", func(c fiber3.Ctx) error { return c.Status(206).SendStream(strings.NewReader("streamed")) }, 206, "streamed", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := router.NewFiberV3Adapter()
			// Inspect on the router unwind path, then make an overwrite decision.
			s.Router().Use(func(next router.HandlerFunc) router.HandlerFunc {
				return func(c router.Context) error {
					if err := next(c); err != nil {
						return err
					}
					state, _ := router.AsResponseState(c)
					size := int64(len(tc.body))
					if tc.stream {
						size = 0
					}
					if state.StatusCode() != tc.code || state.ResponseWritten() != tc.written || state.ResponseBodySize() != size || state.ResponseIsStream() != tc.stream {
						return fmt.Errorf("response state: code=%d written=%t size=%d stream=%t", state.StatusCode(), state.ResponseWritten(), state.ResponseBodySize(), state.ResponseIsStream())
					}
					return nil
				}
			})
			s.Router().Get("/native", func(router.Context) error { return nil }, router.MiddlewareFromFiberV3(tc.mw))
			code, body, _ := v3Response(t, s.WrappedRouter(), "GET", "/native", "")
			if code != tc.code || body != tc.body {
				t.Fatalf("wire: %d %q; want %d %q", code, body, tc.code, tc.body)
			}
		})
	}
}

func TestFiberV3NativeMiddlewareStateBeforeNextAndNestedUnwind(t *testing.T) {
	s := router.NewFiberV3Adapter()
	s.Router().Get("/native", func(c router.Context) error {
		state, _ := router.AsResponseState(c)
		if state.StatusCode() != 201 || !state.ResponseWritten() || state.ResponseBodySize() != 6 {
			return fmt.Errorf("native write was invisible downstream")
		}
		return nil
	}, router.MiddlewareFromFiberV3(func(c fiber3.Ctx) error {
		if err := c.Status(201).SendString("before"); err != nil {
			return err
		}
		return c.Next()
	}), router.MiddlewareFromFiberV3(func(c fiber3.Ctx) error {
		if err := c.Next(); err != nil {
			return err
		}
		return c.Status(202).SendString("after")
	}))
	code, body, _ := v3Response(t, s.WrappedRouter(), "GET", "/native", "")
	if code != 202 || body != "after" {
		t.Fatalf("nested unwind: %d %q", code, body)
	}
}
