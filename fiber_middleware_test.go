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

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

// fiberMiddlewareTrace records which pieces of a request ran, in order. Fiber
// serves app.Test requests on its own goroutines, so access is locked.
type fiberMiddlewareTrace struct {
	mu    sync.Mutex
	steps []string
}

func (tr *fiberMiddlewareTrace) record(step string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.steps = append(tr.steps, step)
}

func (tr *fiberMiddlewareTrace) snapshot() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return slices.Clone(tr.steps)
}

// traceNextFiberMiddleware is a Fiber middleware that calls c.Next() and
// records name before and after it, so tests can see the downstream chain ran
// inside the middleware.
func traceNextFiberMiddleware(tr *fiberMiddlewareTrace, name string) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		tr.record(name)
		err := c.Next()
		tr.record(name + ":after")
		return err
	}
}

// traceClosureMiddleware continues the go-router chain with next(c).
func traceClosureMiddleware(tr *fiberMiddlewareTrace, name string) router.MiddlewareFunc {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			tr.record(name)
			return next(c)
		}
	}
}

// traceIndexMiddleware continues the go-router chain with c.Next().
func traceIndexMiddleware(tr *fiberMiddlewareTrace, name string) router.MiddlewareFunc {
	return func(router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			tr.record(name)
			return c.Next()
		}
	}
}

// traceErrorMiddleware records the error the rest of the chain returned to it.
func traceErrorMiddleware(tr *fiberMiddlewareTrace, name string) router.MiddlewareFunc {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			err := next(c)
			tr.record(fmt.Sprintf("%s:%v", name, err))
			return err
		}
	}
}

// newFiberMiddlewareAdapter returns a Fiber adapter whose app error handler
// prefixes what it renders, so tests can tell it from Fiber's default handler.
func newFiberMiddlewareAdapter() router.Server[*fiber.App] {
	return router.NewFiberAdapter(func(*fiber.App) *fiber.App {
		return fiber.New(fiber.Config{
			ErrorHandler: func(c *fiber.Ctx, err error) error {
				code := http.StatusInternalServerError
				var fiberErr *fiber.Error
				if errors.As(err, &fiberErr) {
					code = fiberErr.Code
				}
				return c.Status(code).SendString("app error: " + err.Error())
			},
		})
	})
}

// serveFiberMiddlewareRequest serves req and returns the response status and
// body. It reports failures as errors instead of through t, so it is safe to
// call from other goroutines.
func serveFiberMiddlewareRequest(app *fiber.App, req *http.Request) (int, string, error) {
	resp, err := app.Test(req, -1)
	if err != nil {
		return 0, "", err
	}
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), errors.Join(err, resp.Body.Close())
}

func TestMiddlewareFromFiber_ContinuesChainOnlyOnNext(t *testing.T) {
	tests := []struct {
		name       string
		middleware func(*fiberMiddlewareTrace) []router.MiddlewareFunc
		handlerErr error
		wantStatus int
		wantBody   string
		wantTrace  []string
	}{
		{
			name: "next runs the chain once",
			middleware: func(tr *fiberMiddlewareTrace) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					router.MiddlewareFromFiber(traceNextFiberMiddleware(tr, "fiber")),
				}
			},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
			wantTrace:  []string{"fiber", "handler", "fiber:after"},
		},
		{
			name: "response without next stops the chain",
			middleware: func(tr *fiberMiddlewareTrace) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					router.MiddlewareFromFiber(func(c *fiber.Ctx) error {
						tr.record("fiber")
						return c.Status(http.StatusUnauthorized).SendString("no")
					}),
					traceClosureMiddleware(tr, "closure"),
					traceIndexMiddleware(tr, "index"),
				}
			},
			wantStatus: http.StatusUnauthorized,
			wantBody:   "no",
			wantTrace:  []string{"fiber"},
		},
		{
			name: "closure and index middleware after it run once",
			middleware: func(tr *fiberMiddlewareTrace) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					router.MiddlewareFromFiber(traceNextFiberMiddleware(tr, "fiber")),
					traceClosureMiddleware(tr, "closure"),
					traceIndexMiddleware(tr, "index"),
				}
			},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
			wantTrace:  []string{"fiber", "closure", "index", "handler", "fiber:after"},
		},
		{
			name: "nested fiber middleware run once each",
			middleware: func(tr *fiberMiddlewareTrace) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					router.MiddlewareFromFiber(traceNextFiberMiddleware(tr, "fiber1")),
					router.MiddlewareFromFiber(traceNextFiberMiddleware(tr, "fiber2")),
				}
			},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
			wantTrace:  []string{"fiber1", "fiber2", "handler", "fiber2:after", "fiber1:after"},
		},
		{
			name: "chain error returns through next",
			middleware: func(tr *fiberMiddlewareTrace) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					traceErrorMiddleware(tr, "upstream"),
					router.MiddlewareFromFiber(traceNextFiberMiddleware(tr, "fiber")),
				}
			},
			handlerErr: fiber.NewError(http.StatusTeapot, "teapot"),
			wantStatus: http.StatusTeapot,
			wantBody:   "app error: teapot",
			wantTrace:  []string{"fiber", "handler", "fiber:after", "upstream:teapot"},
		},
		{
			name: "fiber middleware error returns to the chain",
			middleware: func(tr *fiberMiddlewareTrace) []router.MiddlewareFunc {
				return []router.MiddlewareFunc{
					traceErrorMiddleware(tr, "upstream"),
					router.MiddlewareFromFiber(func(*fiber.Ctx) error {
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
			tr := &fiberMiddlewareTrace{}
			adapter := newFiberMiddlewareAdapter()
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
			status, body, err := serveFiberMiddlewareRequest(adapter.WrappedRouter(), req)
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
func TestMiddlewareFromFiber_ConcurrentRequestsKeepOwnState(t *testing.T) {
	adapter := newFiberMiddlewareAdapter()
	r := adapter.Router()

	var handled atomic.Int64
	r.Use(router.MiddlewareFromFiber(func(c *fiber.Ctx) error {
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
			status, body, err := serveFiberMiddlewareRequest(app, req)
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
