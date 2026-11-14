package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f2 "github.com/gofiber/fiber/v2"
	f3 "github.com/gofiber/fiber/v3"
)

// The test adapter only shares assertions. Every request and middleware chain
// still runs through the selected native Fiber app, including Next and Route.
type nativeMiddlewareContext struct {
	next  func() error
	route func() any
	send  func(string) error
}
type nativeMiddlewareHandler func(nativeMiddlewareContext) error
type nativeMiddlewareFixture struct {
	managedFixture
	use, terminal func(...nativeMiddlewareHandler) error
	test          func(*http.Request) (*http.Response, error)
	configure     func(BoundedIngressOptions) error
	handle        func(HTTPMethod, string, HandlerFunc)
	static        func(string)
	miss          func(HTTPMethod, HandlerFunc)
	validator     func(string, func() error) error
	snapshot      func() RegistrationSnapshot
	root          *routerRoot
	guard         *nativeRegistrationGuard
	hook          func(string)
	copyTerminal  func() error
}

func nativeMiddlewareFixtureFor(t *testing.T, major string, immediate bool) nativeMiddlewareFixture {
	t.Helper()
	cfg := FiberAdapterConfig{PreserveRegistrationOrder: immediate, StrictRoutes: true}
	var f nativeMiddlewareFixture
	if major == "v2" {
		app := f2.New(f2.Config{DisableStartupMessage: true, RequestMethods: append(slices.Clone(f2.DefaultMethods), "PURGE")})
		s := NewFiberAdapterWithConfig(cfg, func(*f2.App) *f2.App { return app }).(*FiberAdapter)
		convert := func(hs []nativeMiddlewareHandler) []f2.Handler {
			out := make([]f2.Handler, len(hs))
			for i, h := range hs {
				if h != nil {
					out[i] = func(c *f2.Ctx) error {
						return h(nativeMiddlewareContext{c.Next, func() any { return c.Route() }, c.SendString})
					}
				}
			}
			return out
		}
		f.use = func(hs ...nativeMiddlewareHandler) error { return s.UseNative(convert(hs)...) }
		f.terminal = func(hs ...nativeMiddlewareHandler) error { return s.UseNativeTerminal(convert(hs)...) }
		f.test = func(req *http.Request) (*http.Response, error) { return s.WrappedRouter().Test(req) }
		f.configure = s.ConfigureBoundedIngress
		f.handle = func(m HTTPMethod, p string, h HandlerFunc) { s.Router().Group("/api").Handle(m, p, h) }
		f.static = func(dir string) { s.Router().Group("/api").Static("/assets", dir) }
		f.miss = func(m HTTPMethod, h HandlerFunc) { s.router.HandleMiss(m, h) }
		f.validator = s.router.RegisterStartupValidator
		f.snapshot = s.router.RegistrationSnapshot
		f.root = s.router.root
		f.guard = s.router.ownership
		f.managedFixture = managedFixture{server: s, initialize: s.Initialize, serve: s.Serve, shutdown: s.Shutdown, raw: func() { s.WrappedRouter() }}
		f.hook = func(kind string) {
			app.Hooks().OnRoute(func(r f2.Route) error {
				switch kind {
				case "path":
					for _, stack := range app.Stack() {
						for _, entry := range stack {
							entry.Path = "/tampered"
						}
					}
				case "name":
					for _, stack := range app.Stack() {
						for _, entry := range stack {
							entry.Name = "tampered"
						}
					}
				case "handler":
					r.Handlers[0] = func(c *f2.Ctx) error { return c.SendString("tampered") }
				case "panic":
					panic("registration hook")
				}
				return nil
			})
		}
		f.copyTerminal = func() error {
			hs := []f2.Handler{func(c *f2.Ctx) error { return c.SendString("copied") }}
			err := s.UseNativeTerminal(hs...)
			hs[0] = func(c *f2.Ctx) error { return c.SendString("mutated") }
			return err
		}
	} else {
		app := f3.New(f3.Config{RequestMethods: append(slices.Clone(f3.DefaultMethods), "PURGE")})
		s := NewFiberV3AdapterWithConfig(FiberV3AdapterConfig{FiberAdapterConfig: cfg, ListenConfig: f3.ListenConfig{DisableStartupMessage: true}}, func(*f3.App) *f3.App { return app }).(*FiberV3Adapter)
		convert := func(hs []nativeMiddlewareHandler) []f3.Handler {
			out := make([]f3.Handler, len(hs))
			for i, h := range hs {
				if h != nil {
					out[i] = func(c f3.Ctx) error {
						return h(nativeMiddlewareContext{c.Next, func() any { return c.Route() }, c.SendString})
					}
				}
			}
			return out
		}
		f.use = func(hs ...nativeMiddlewareHandler) error { return s.UseNative(convert(hs)...) }
		f.terminal = func(hs ...nativeMiddlewareHandler) error { return s.UseNativeTerminal(convert(hs)...) }
		f.test = func(req *http.Request) (*http.Response, error) { return s.WrappedRouter().Test(req) }
		f.configure = s.ConfigureBoundedIngress
		f.handle = func(m HTTPMethod, p string, h HandlerFunc) { s.Router().Group("/api").Handle(m, p, h) }
		f.static = func(dir string) { s.Router().Group("/api").Static("/assets", dir) }
		f.miss = func(m HTTPMethod, h HandlerFunc) { s.router.HandleMiss(m, h) }
		f.validator = s.router.RegisterStartupValidator
		f.snapshot = s.router.RegistrationSnapshot
		f.root = s.router.root
		f.guard = s.router.ownership
		f.managedFixture = managedFixture{server: s, initialize: s.Initialize, serve: s.Serve, shutdown: s.Shutdown, raw: func() { s.WrappedRouter() }}
		f.hook = func(kind string) {
			app.Hooks().OnRoute(func(r f3.Route) error {
				switch kind {
				case "path":
					for _, stack := range app.Stack() {
						for _, entry := range stack {
							entry.Path = "/tampered"
						}
					}
				case "name":
					for _, stack := range app.Stack() {
						for _, entry := range stack {
							entry.Name = "tampered"
						}
					}
				case "handler":
					r.Handlers[0] = func(c f3.Ctx) error { return c.SendString("tampered") }
				case "panic":
					panic("registration hook")
				}
				return nil
			})
		}
		f.copyTerminal = func() error {
			hs := []f3.Handler{func(c f3.Ctx) error { return c.SendString("copied") }}
			err := s.UseNativeTerminal(hs...)
			hs[0] = func(c f3.Ctx) error { return c.SendString("mutated") }
			return err
		}
	}
	return f
}

func nativeMiddlewareMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func nativeMiddlewareResponse(t *testing.T, req *http.Request, request func(*http.Request) (*http.Response, error), status int, body string) {
	t.Helper()
	res, err := request(req)
	nativeMiddlewareMust(t, err)
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	nativeMiddlewareMust(t, err)
	if res.StatusCode != status || string(b) != body {
		t.Fatalf("%s %s: status=%d body=%q, want %d %q", req.Method, req.URL, res.StatusCode, b, status, body)
	}
}

func TestNativeMiddlewareOrdering(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, immediate := range []bool{false, true} {
			for _, socket := range []bool{false, true} {
				for _, bounded := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/immediate=%v/socket=%v/bounded=%v", major, immediate, socket, bounded), func(t *testing.T) {
						f := nativeMiddlewareFixtureFor(t, major, immediate)
						var mu sync.Mutex
						var calls []string
						record := func(s string) { mu.Lock(); defer mu.Unlock(); calls = append(calls, s) }
						middleware := func(label string) nativeMiddlewareHandler {
							return func(c nativeMiddlewareContext) error {
								record(label + " before")
								err := c.next()
								record(label + " after")
								return err
							}
						}
						nativeMiddlewareMust(t, f.use(middleware("early1"), middleware("early2")))
						// Queue before endpoints to detect accidental immediate terminal installation.
						nativeMiddlewareMust(t, f.terminal(middleware("terminal1")))
						nativeMiddlewareMust(t, f.terminal(func(c nativeMiddlewareContext) error { record("terminal2"); return c.send("fallback") }))
						f.handle(GET, "/items/:id", func(c Context) error { record("endpoint"); return c.SendString("endpoint") })
						f.handle(HEAD, "/items/:id", func(c Context) error { record("endpoint"); return c.SendString("endpoint") })
						f.handle(HTTPMethod("PURGE"), "/cache", func(c Context) error { record("purge"); return c.SendString("purge") })
						f.handle(HTTPMethod("OPTIONS"), "/options", func(c Context) error { record("options"); return c.SendString("options") })
						dir := t.TempDir()
						nativeMiddlewareMust(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("static"), 0600))
						f.static(dir)
						var validators atomic.Int32
						nativeMiddlewareMust(t, f.validator("complete", func() error {
							validators.Add(1)
							snap := f.snapshot()
							if snap.State != RegistrationSealed || len(snap.MountedRoutes) != len(snap.DeclaredRoutes) {
								return fmt.Errorf("incomplete publication: %+v", snap)
							}
							for _, r := range snap.MountedRoutes {
								if r.Path == "/" {
									return errors.New("native middleware fabricated an endpoint")
								}
							}
							if len(f.root.nativeMiddleware.terminal) != 0 {
								return errors.New("terminal queue not installed before validation")
							}
							return f.guard.validate()
						}))
						if bounded {
							nativeMiddlewareMust(t, f.configure(BoundedIngressOptions{}))
						}
						var wg sync.WaitGroup
						for range 8 {
							wg.Add(1)
							go func() {
								defer wg.Done()
								if err := f.initialize(); err != nil {
									t.Error(err)
								}
							}()
						}
						wg.Wait()
						if validators.Load() != 1 {
							t.Fatalf("validator calls=%d", validators.Load())
						}
						request := f.test
						base := "http://example.test"
						if socket {
							run := managedStart(t, f.managedFixture, context.Background(), managedOptions())
							managedReady(t, run)
							base = "http://" + run.Address()
							client := &http.Client{Timeout: time.Second}
							request = client.Do
						}
						for _, tc := range []struct{ method, path, body, event string }{{"GET", "/api/items/one", "endpoint", "endpoint"}, {"HEAD", "/api/items/one", "", "endpoint"}, {"PURGE", "/api/cache", "purge", "purge"}, {"OPTIONS", "/api/options", "options", "options"}, {"GET", "/api/assets/file.txt", "static", ""}, {"GET", "/missing/deep", "fallback", "terminal1 before"}} {
							mu.Lock()
							calls = nil
							mu.Unlock()
							nativeMiddlewareResponse(t, httptest.NewRequest(tc.method, base+tc.path, nil), func(req *http.Request) (*http.Response, error) { req.RequestURI = ""; return request(req) }, 200, tc.body)
							want := []string{"early1 before", "early2 before"}
							if tc.event != "" {
								want = append(want, tc.event)
							}
							if tc.event == "terminal1 before" {
								want = append(want, "terminal2", "terminal1 after")
							}
							want = append(want, "early2 after", "early1 after")
							mu.Lock()
							got := slices.Clone(calls)
							mu.Unlock()
							if !reflect.DeepEqual(got, want) {
								t.Fatalf("%s %s order=%v want=%v", tc.method, tc.path, got, want)
							}
						}
					})
				}
			}
		}
	}
}

func TestNativeMiddlewareMissAndProvenance(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major+"/caller_miss", func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			var terminal atomic.Int32
			nativeMiddlewareMust(t, f.terminal(func(c nativeMiddlewareContext) error { terminal.Add(1); return c.send("terminal") }))
			f.miss(GET, func(c Context) error { return c.SendString("caller miss") })
			nativeMiddlewareResponse(t, httptest.NewRequest("GET", "/missing", nil), f.test, 200, "caller miss")
			if terminal.Load() != 0 {
				t.Fatal("terminal overtook caller miss")
			}
		})
		t.Run(major+"/native_terminal", func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			var terminal atomic.Int32
			nativeMiddlewareMust(t, f.terminal(func(c nativeMiddlewareContext) error {
				terminal.Add(1)
				before := c.route()
				err := c.next()
				if c.route() != before {
					t.Error("terminal native Route provenance changed")
				}
				return err
			}))
			f.handle(GET, "/known", func(c Context) error { return c.SendString("known") })
			nativeMiddlewareResponse(t, httptest.NewRequest("POST", "/api/known", nil), func(req *http.Request) (*http.Response, error) {
				res, err := f.test(req)
				if err == nil {
					want := "GET"
					if major == "v3" {
						want = "GET, HEAD"
					}
					if res.Header.Get("Allow") != want {
						t.Errorf("Allow=%q want %q", res.Header.Get("Allow"), want)
					}
				}
				return res, err
			}, 405, "Method Not Allowed")
			if terminal.Load() != 1 {
				t.Fatal("terminal not called exactly once")
			}
		})
	}
}

func TestNativeMiddlewareValidation(t *testing.T) {
	pass := func(c nativeMiddlewareContext) error { return c.next() }
	for _, major := range []string{"v2", "v3"} {
		for _, kind := range []string{"endpoint", "static", "miss"} {
			t.Run(major+"/order/"+kind, func(t *testing.T) {
				f := nativeMiddlewareFixtureFor(t, major, false)
				switch kind {
				case "endpoint":
					f.handle(GET, "/ok", func(c Context) error { return c.SendString("ok") })
				case "static":
					f.static(t.TempDir())
				case "miss":
					f.miss(GET, func(c Context) error { return c.SendString("miss") })
				}
				err := f.use(pass)
				var typed *RegistrationError
				if !errors.Is(err, ErrNativeMiddlewareOrder) || !errors.As(err, &typed) {
					t.Fatalf("order error=%v", err)
				}
				nativeMiddlewareMust(t, f.use())
				nativeMiddlewareMust(t, f.terminal(pass))
				nativeMiddlewareMust(t, f.initialize())
			})
		}
		for _, terminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/nil/terminal=%v", major, terminal), func(t *testing.T) {
				f := nativeMiddlewareFixtureFor(t, major, false)
				register := f.use
				if terminal {
					register = f.terminal
				}
				before := f.guard.capture()
				err := register(pass, nil, pass)
				if !errors.Is(err, ErrNativeMiddlewareNil) {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, f.guard.capture()) || len(f.root.nativeMiddleware.terminal) != 0 {
					t.Fatal("nil list partially registered")
				}
				nativeMiddlewareMust(t, register())
				nativeMiddlewareMust(t, register(pass))
				nativeMiddlewareMust(t, f.initialize())
				for _, call := range []func(...nativeMiddlewareHandler) error{f.use, f.terminal} {
					for _, hs := range [][]nativeMiddlewareHandler{nil, {pass}, {nil}} {
						if err := call(hs...); !errors.Is(err, ErrRouterSealed) {
							t.Fatalf("late registration=%v", err)
						}
					}
				}
			})
		}
		t.Run(major+"/copy", func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			nativeMiddlewareMust(t, f.copyTerminal())
			nativeMiddlewareResponse(t, httptest.NewRequest("GET", "/missing", nil), f.test, 200, "copied")
		})
	}
}

func TestNativeMiddlewareHookFailure(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, terminal := range []bool{false, true} {
			for _, kind := range []string{"path", "name", "handler", "panic"} {
				t.Run(fmt.Sprintf("%s/terminal=%v/%s", major, terminal, kind), func(t *testing.T) {
					f := nativeMiddlewareFixtureFor(t, major, false)
					f.hook(kind)
					register := f.use
					if terminal {
						register = f.terminal
					}
					err := register(func(c nativeMiddlewareContext) error { return c.next() })
					if terminal {
						nativeMiddlewareMust(t, err)
					} else if !errors.Is(err, ErrManagedNativeRegistration) {
						t.Fatalf("registration error=%v", err)
					}
					// Further declarations must not reenter Fiber after a hook failure.
					if !terminal {
						func() {
							defer func() {
								value := recover()
								err, ok := value.(error)
								if !ok || !errors.Is(err, ErrRouterSealed) {
									t.Errorf("registration after native failure: %v", value)
								}
							}()
							f.static(t.TempDir())
						}()
					}
					first := f.initialize()
					var startup *StartupValidationError
					if !errors.Is(first, ErrManagedNativeRegistration) || !errors.As(first, &startup) {
						t.Fatalf("startup error=%v", first)
					}
					if kind == "panic" {
						var p *ManagedPanicError
						if !errors.As(first, &p) {
							t.Fatalf("panic cause lost: %v", first)
						}
					}
					if next := f.initialize(); next != first {
						t.Fatalf("failure not sticky: %v / %v", first, next)
					}
					if err := f.serve("127.0.0.1:0"); err != first {
						t.Fatalf("Serve error=%v", err)
					}
					run := managedStart(t, f.managedFixture, context.Background(), managedOptions())
					managedAwait(t, run.Done())
					if !errors.Is(run.Wait(context.Background()), ErrManagedNativeRegistration) {
						t.Fatal("managed startup lost cause")
					}
					select {
					case <-run.Bound():
						t.Fatal("bound despite failure")
					default:
					}
				})
			}
		}
	}
}

func TestNativeMiddlewareStartupGate(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major+"/pre_canceled", func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			nativeMiddlewareMust(t, f.copyTerminal())
			var calls atomic.Int32
			nativeMiddlewareMust(t, f.validator("count", func() error { calls.Add(1); return nil }))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			run, err := f.server.StartManaged(ctx, managedOptions())
			if !errors.Is(err, context.Canceled) || run != nil {
				t.Fatalf("run=%v err=%v", run, err)
			}
			if calls.Load() != 0 || len(f.guard.capture()) != 0 || f.snapshot().State != RegistrationCollecting {
				t.Fatal("pre-cancel performed initialization")
			}
			if err := f.terminal(); !errors.Is(err, ErrRouterSealed) {
				t.Fatalf("start did not close native setup: %v", err)
			}
			// Pre-cancel does not reserve the managed lifetime; prepared declarations survive.
			run = managedStart(t, f.managedFixture, context.Background(), managedOptions())
			managedReady(t, run)
			if got := managedRequest(t, run.Address(), "/missing"); got != "copied" {
				t.Fatal(got)
			}
		})
		t.Run(major+"/async_start", func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			nativeMiddlewareMust(t, f.validator("pause", func() error { close(entered); <-release; return nil }))
			run := managedStart(t, f.managedFixture, context.Background(), managedOptions())
			managedAwait(t, entered)
			if err := f.use(); !errors.Is(err, ErrRouterSealed) {
				t.Fatal(err)
			}
			if err := f.terminal(); !errors.Is(err, ErrRouterSealed) {
				t.Fatal(err)
			}
			unblock()
			managedReady(t, run)
		})
		t.Run(major+"/raw_excludes_managed", func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			nativeMiddlewareMust(t, f.copyTerminal())
			f.raw()
			if run, err := f.server.StartManaged(context.Background(), managedOptions()); !errors.Is(err, ErrHostingMode) || run != nil {
				t.Fatalf("run=%v err=%v", run, err)
			}
		})
	}
}

func TestNativeMiddlewareOwnershipAndFailureOrder(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, terminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/unowned/terminal=%v", major, terminal), func(t *testing.T) {
				f := nativeMiddlewareFixtureFor(t, major, false)
				switch a := f.server.(type) {
				case *FiberAdapter:
					a.app.Use(func(c *f2.Ctx) error { return c.Next() })
				case *FiberV3Adapter:
					a.app.Use(func(c f3.Ctx) error { return c.Next() })
				}
				register := f.use
				if terminal {
					register = f.terminal
				}
				err := register(func(c nativeMiddlewareContext) error { return c.next() })
				if terminal {
					nativeMiddlewareMust(t, err)
				} else if !errors.Is(err, ErrManagedNativeRegistration) {
					t.Fatal(err)
				}
				if err := f.initialize(); !errors.Is(err, ErrManagedNativeRegistration) {
					t.Fatalf("unowned registration laundered: %v", err)
				}
			})
		}
		t.Run(major+"/strict_before_terminal", func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			nativeMiddlewareMust(t, f.copyTerminal())
			f.handle(GET, "/nil", nil)
			err := f.initialize()
			var startup *StartupValidationError
			if !errors.As(err, &startup) {
				t.Fatalf("missing strict failure: %v", err)
			}
			if len(f.guard.capture()) != 0 {
				t.Fatal("native endpoints/terminal installed before strict rejection")
			}
			if err2 := f.initialize(); err2 != err {
				t.Fatalf("strict failure not sticky: %v", err2)
			}
		})
		t.Run(major+"/validator_after_terminal", func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			nativeMiddlewareMust(t, f.copyTerminal())
			cause := errors.New("validator denied")
			nativeMiddlewareMust(t, f.validator("reject", func() error {
				if len(f.guard.capture()) == 0 {
					t.Error("terminal missing before validator")
				}
				if err := f.terminal(); !errors.Is(err, ErrRouterSealed) {
					t.Errorf("validator could change native registration: %v", err)
				}
				return cause
			}))
			if err := f.initialize(); !errors.Is(err, cause) {
				t.Fatalf("validator cause lost: %v", err)
			}
			run := managedStart(t, f.managedFixture, context.Background(), managedOptions())
			managedAwait(t, run.Done())
			if err := run.Wait(context.Background()); !errors.Is(err, cause) {
				t.Fatal(err)
			}
			select {
			case <-run.Bound():
				t.Fatal("bound despite validator failure")
			default:
			}
		})
	}
}

func TestNativeMiddlewareDefaultApps(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			var server ManagedServer
			if major == "v2" {
				s := NewFiberAdapter()
				api, ok := s.(FiberNativeMiddlewareRegistrar)
				if !ok {
					t.Fatal("optional v2 capability missing")
				}
				nativeMiddlewareMust(t, api.UseNative(func(c *f2.Ctx) error { return c.Next() }))
				nativeMiddlewareMust(t, api.UseNativeTerminal(func(c *f2.Ctx) error { return c.Next() }))
				s.Router().Get("/ok", func(c Context) error { return c.SendString("ok") })
				server = s.(ManagedServer)
			} else {
				s := NewFiberV3Adapter()
				api, ok := s.(FiberV3NativeMiddlewareRegistrar)
				if !ok {
					t.Fatal("optional v3 capability missing")
				}
				nativeMiddlewareMust(t, api.UseNative(func(c f3.Ctx) error { return c.Next() }))
				nativeMiddlewareMust(t, api.UseNativeTerminal(func(c f3.Ctx) error { return c.Next() }))
				s.Router().Get("/ok", func(c Context) error { return c.SendString("ok") })
				server = s.(ManagedServer)
			}
			run := managedStart(t, managedFixture{server: server}, context.Background(), managedOptions())
			managedReady(t, run)
			if got := managedRequest(t, run.Address(), "/ok"); got != "ok" {
				t.Fatal(got)
			}
			if major == "v3" {
				client := http.Client{Timeout: time.Second}
				nativeMiddlewareResponse(t, httptest.NewRequest("HEAD", "http://"+run.Address()+"/ok", nil), func(r *http.Request) (*http.Response, error) { r.RequestURI = ""; return client.Do(r) }, 200, "")
			}
		})
	}
}

func TestNativeMiddlewareLegacyServe(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := nativeMiddlewareFixtureFor(t, major, false)
			var middlewareCalls atomic.Int32
			nativeMiddlewareMust(t, f.use(func(c nativeMiddlewareContext) error { middlewareCalls.Add(1); return c.next() }))
			nativeMiddlewareMust(t, f.copyTerminal())
			listening := make(chan string, 1)
			switch s := f.server.(type) {
			case *FiberAdapter:
				s.app.Hooks().OnListen(func(data f2.ListenData) error { listening <- net.JoinHostPort(data.Host, data.Port); return nil })
			case *FiberV3Adapter:
				s.app.Hooks().OnListen(func(data f3.ListenData) error { listening <- net.JoinHostPort(data.Host, data.Port); return nil })
			}
			done := make(chan error, 1)
			go func() { done <- f.serve("127.0.0.1:0") }()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := f.shutdown(ctx); err != nil {
					t.Errorf("legacy shutdown: %v", err)
				}
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("legacy serve: %v", err)
					}
				case <-ctx.Done():
					t.Error("legacy serve did not join")
				}
			})
			var address string
			select {
			case address = <-listening:
			case err := <-done:
				done <- err
				t.Fatalf("legacy startup: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("legacy startup timed out")
			}
			if got := managedRequest(t, address, "/missing"); got != "copied" {
				t.Fatal(got)
			}
			if middlewareCalls.Load() != 1 {
				t.Fatalf("early middleware calls=%d", middlewareCalls.Load())
			}
			if err := f.terminal(); !errors.Is(err, ErrRouterSealed) {
				t.Fatalf("legacy serve left registration open: %v", err)
			}
		})
	}
}
