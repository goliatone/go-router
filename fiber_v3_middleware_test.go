package router_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/goliatone/go-router"
)

// fiberMiddlewareTraceV3 records which pieces of a request ran, in order. Fiber
// serves app.Test requests on its own goroutines, so access is locked.
type fiberMiddlewareTraceV3 struct {
	mu    sync.Mutex
	steps []string
}

func (tr *fiberMiddlewareTraceV3) record(step string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.steps = append(tr.steps, step)
}

func (tr *fiberMiddlewareTraceV3) snapshot() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return slices.Clone(tr.steps)
}

// traceNextFiberMiddlewareV3 is a Fiber middleware that calls c.Next() and
// records name before and after it, so tests can see the downstream chain ran
// inside the middleware.
func traceNextFiberMiddlewareV3(tr *fiberMiddlewareTraceV3, name string) func(fiber.Ctx) error {
	return func(c fiber.Ctx) error {
		tr.record(name)
		err := c.Next()
		tr.record(name + ":after")
		return err
	}
}

// traceClosureMiddlewareV3 continues the go-router chain with next(c).
func traceClosureMiddlewareV3(tr *fiberMiddlewareTraceV3, name string) router.MiddlewareFunc {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			tr.record(name)
			return next(c)
		}
	}
}

// traceIndexMiddlewareV3 continues the go-router chain with c.Next().
func traceIndexMiddlewareV3(tr *fiberMiddlewareTraceV3, name string) router.MiddlewareFunc {
	return func(router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			tr.record(name)
			return c.Next()
		}
	}
}

// traceErrorMiddlewareV3 records the error the rest of the chain returned to it.
func traceErrorMiddlewareV3(tr *fiberMiddlewareTraceV3, name string) router.MiddlewareFunc {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			err := next(c)
			tr.record(fmt.Sprintf("%s:%v", name, err))
			return err
		}
	}
}

// newFiberMiddlewareAdapterV3 returns a Fiber adapter whose app error handler
// prefixes what it renders, so tests can tell it from Fiber's default handler.
func newFiberMiddlewareAdapterV3() router.Server[*fiber.App] {
	return router.NewFiberV3Adapter(func(*fiber.App) *fiber.App {
		return fiber.New(fiber.Config{
			ErrorHandler: func(c fiber.Ctx, err error) error {
				code := http.StatusInternalServerError
				if fiberErr, ok := errors.AsType[*fiber.Error](err); ok {
					code = fiberErr.Code
				}
				return c.Status(code).SendString("app error: " + err.Error())
			},
		})
	})
}

// serveFiberMiddlewareRequestV3 serves req and returns the response status and
// body. It reports failures as errors instead of through t, so it is safe to
// call from other goroutines.
func serveFiberMiddlewareRequestV3(app *fiber.App, req *http.Request) (int, string, error) {
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		return 0, "", err
	}
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), errors.Join(err, resp.Body.Close())
}

func TestMiddlewareFromFiber_ContinuesChainOnlyOnNextV3(t *testing.T) {
	tests := []struct {
		name       string
		middleware func(*fiberMiddlewareTraceV3) []router.MiddlewareFunc
		handlerErr error
		wantStatus int
		wantBody   string
		wantTrace  []string
	}{
		{
			name: "next runs the chain once",
			middleware: func(tr *fiberMiddlewareTraceV3) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					router.MiddlewareFromFiberV3(traceNextFiberMiddlewareV3(tr, "fiber")),
				}
			},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
			wantTrace:  []string{"fiber", "handler", "fiber:after"},
		},
		{
			name: "response without next stops the chain",
			middleware: func(tr *fiberMiddlewareTraceV3) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					router.MiddlewareFromFiberV3(func(c fiber.Ctx) error {
						tr.record("fiber")
						return c.Status(http.StatusUnauthorized).SendString("no")
					}),
					traceClosureMiddlewareV3(tr, "closure"),
					traceIndexMiddlewareV3(tr, "index"),
				}
			},
			wantStatus: http.StatusUnauthorized,
			wantBody:   "no",
			wantTrace:  []string{"fiber"},
		},
		{
			name: "closure and index middleware after it run once",
			middleware: func(tr *fiberMiddlewareTraceV3) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					router.MiddlewareFromFiberV3(traceNextFiberMiddlewareV3(tr, "fiber")),
					traceClosureMiddlewareV3(tr, "closure"),
					traceIndexMiddlewareV3(tr, "index"),
				}
			},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
			wantTrace:  []string{"fiber", "closure", "index", "handler", "fiber:after"},
		},
		{
			name: "nested fiber middleware run once each",
			middleware: func(tr *fiberMiddlewareTraceV3) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					router.MiddlewareFromFiberV3(traceNextFiberMiddlewareV3(tr, "fiber1")),
					router.MiddlewareFromFiberV3(traceNextFiberMiddlewareV3(tr, "fiber2")),
				}
			},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
			wantTrace:  []string{"fiber1", "fiber2", "handler", "fiber2:after", "fiber1:after"},
		},
		{
			name: "chain error returns through next",
			middleware: func(tr *fiberMiddlewareTraceV3) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					traceErrorMiddlewareV3(tr, "upstream"),
					router.MiddlewareFromFiberV3(traceNextFiberMiddlewareV3(tr, "fiber")),
				}
			},
			handlerErr: fiber.NewError(http.StatusTeapot, "teapot"),
			wantStatus: http.StatusTeapot,
			wantBody:   "app error: teapot",
			wantTrace:  []string{"fiber", "handler", "fiber:after", "upstream:teapot"},
		},
		{
			name: "fiber middleware error returns to the chain",
			middleware: func(tr *fiberMiddlewareTraceV3) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					traceErrorMiddlewareV3(tr, "upstream"),
					router.MiddlewareFromFiberV3(func(fiber.Ctx) error {
						tr.record("fiber")
						return fiber.ErrUnauthorized
					}),
				}
			},
			wantStatus: http.StatusUnauthorized,
			wantBody:   "app error: Unauthorized",
			wantTrace:  []string{"fiber", "upstream:Unauthorized"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &fiberMiddlewareTraceV3{}
			adapter := newFiberMiddlewareAdapterV3()
			r := adapter.Router()
			r.Use(tt.middleware(tr)...)
			r.Get("/chain", func(c router.Context) error {
				tr.record("handler")
				if tt.handlerErr != nil {
					return tt.handlerErr
				}
				return c.SendString("ok")
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/chain", nil)
			status, body, err := serveFiberMiddlewareRequestV3(adapter.WrappedRouter(), req)
			if err != nil {
				t.Fatalf("GET /chain: %v", err)
			}
			if status != tt.wantStatus || body != tt.wantBody {
				t.Errorf("response = %d %q, want %d %q", status, body, tt.wantStatus, tt.wantBody)
			}
			if got := tr.snapshot(); !slices.Equal(got, tt.wantTrace) {
				t.Errorf("trace = %v, want %v", got, tt.wantTrace)
			}
		})
	}
}

// All adapters share one Fiber sub-app, so concurrent requests must each keep
// their own middleware state.
func TestMiddlewareFromFiber_ConcurrentRequestsKeepOwnStateV3(t *testing.T) {
	adapter := newFiberMiddlewareAdapterV3()
	r := adapter.Router()

	var handled atomic.Int64
	r.Use(router.MiddlewareFromFiberV3(func(c fiber.Ctx) error {
		c.Locals("request_id", c.Get("X-Request-ID"))
		return c.Next()
	}))
	r.Get("/concurrent", func(c router.Context) error {
		handled.Add(1)
		id, ok := c.Locals("request_id").(string)
		if !ok {
			return errors.New("request_id local not set")
		}
		return c.SendString(id)
	})
	app := adapter.WrappedRouter()

	const requests = 32
	ctx := t.Context()
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() {
			id := strconv.Itoa(i)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/concurrent", nil)
			req.Header.Set("X-Request-ID", id)
			status, body, err := serveFiberMiddlewareRequestV3(app, req)
			if err != nil {
				t.Errorf("request %s: %v", id, err)
				return
			}
			if status != http.StatusOK || body != id {
				t.Errorf("request %s: response = %d %q, want %d %q", id, status, body, http.StatusOK, id)
			}
		})
	}
	wg.Wait()

	if got := handled.Load(); got != requests {
		t.Errorf("handler ran %d times for %d requests", got, requests)
	}
}
