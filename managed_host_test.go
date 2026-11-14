package router

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	f2 "github.com/gofiber/fiber/v2"
	f3 "github.com/gofiber/fiber/v3"
	"github.com/gorilla/websocket"
	"github.com/valyala/fasthttp"
)

type managedFixture struct {
	server      ManagedServer
	initialize  func() error
	serve       func(string) error
	shutdown    func(context.Context) error
	raw         func()
	get         func(string, HandlerFunc)
	nativeGet   func(string)
	nativeUse   func()
	nativeMount func()
	onListen    func(func() error)
	beforeServe func(func() error)
}

func managedFixtureFor(t *testing.T, major string, cfg FiberAdapterConfig) managedFixture {
	t.Helper()
	if major == "v2" {
		app := f2.New(f2.Config{DisableStartupMessage: true})
		s := NewFiberAdapterWithConfig(cfg, func(*f2.App) *f2.App { return app }).(*FiberAdapter)
		return managedFixture{server: s, initialize: s.Initialize, serve: s.Serve, shutdown: s.Shutdown, raw: func() { s.WrappedRouter() },
			get:       func(path string, h HandlerFunc) { s.Router().Group("/api").Get(path, h) },
			nativeGet: func(path string) { app.Get(path, func(c *f2.Ctx) error { return c.SendString("raw") }) },
			nativeUse: func() { app.Use(func(c *f2.Ctx) error { return c.Next() }) },
			nativeMount: func() {
				sub := f2.New()
				sub.Get("/", func(c *f2.Ctx) error { return c.SendString("raw") })
				app.Mount("/raw", sub)
			},
			onListen: func(fn func() error) { app.Hooks().OnListen(func(f2.ListenData) error { return fn() }) }}
	}
	app := f3.New()
	s := NewFiberV3AdapterWithConfig(FiberV3AdapterConfig{FiberAdapterConfig: cfg, ListenConfig: f3.ListenConfig{DisableStartupMessage: true}}, func(*f3.App) *f3.App { return app }).(*FiberV3Adapter)
	return managedFixture{server: s, initialize: s.Initialize, serve: s.Serve, shutdown: s.Shutdown, raw: func() { s.WrappedRouter() },
		get:       func(path string, h HandlerFunc) { s.Router().Group("/api").Get(path, h) },
		nativeGet: func(path string) { app.Get(path, func(c f3.Ctx) error { return c.SendString("raw") }) },
		nativeUse: func() { app.Use(func(c f3.Ctx) error { return c.Next() }) },
		nativeMount: func() {
			sub := f3.New()
			sub.Get("/", func(c f3.Ctx) error { return c.SendString("raw") })
			app.Use("/raw", sub)
		},
		onListen:    func(fn func() error) { app.Hooks().OnListen(func(f3.ListenData) error { return fn() }) },
		beforeServe: func(fn func() error) { s.listenConfig.BeforeServeFunc = func(*f3.App) error { return fn() } }}
}
func managedAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("completion signal timed out")
	}
}
func managedOptions() ManagedOptions {
	return ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second}
}
func managedStart(t *testing.T, f managedFixture, parent context.Context, opts ManagedOptions) ManagedRun {
	t.Helper()
	run, err := f.server.StartManaged(parent, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = run.Stop(ctx)
		if err := run.Wait(ctx); errors.Is(err, ctx.Err()) && ctx.Err() != nil {
			t.Errorf("run did not join: %v", err)
		}
	})
	return run
}
func managedReady(t *testing.T, run ManagedRun) {
	t.Helper()
	select {
	case <-run.Serving():
	case <-run.Done():
		t.Fatalf("startup: %v", run.Wait(context.Background()))
	case <-time.After(3 * time.Second):
		t.Fatal("serving timed out")
	}
}
func managedRequest(t *testing.T, address, path string) string {
	t.Helper()
	client := http.Client{Timeout: time.Second}
	res, err := client.Get("http://" + address + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	return string(body)
}
func TestManagedHosting(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			t.Run("bound-serving-context-single-use", func(t *testing.T) {
				f := managedFixtureFor(t, major, FiberAdapterConfig{})
				type key struct{}
				requestContexts := make(chan context.Context, 1)
				f.get("/ok", func(c Context) error {
					requestContexts <- c.Context()
					if c.Context().Value(key{}) != "parent" {
						return errors.New("lost parent value")
					}
					return c.SendString("ok")
				})
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				opts := managedOptions()
				opts.OnBound = func(ctx context.Context, addr string) error { close(entered); <-release; return nil }
				run := managedStart(t, f, context.WithValue(context.Background(), key{}, "parent"), opts)
				defer unblock()
				managedAwait(t, entered)
				if run.Address() == "" || run.State() != ManagedBound {
					t.Fatal("not bound")
				}
				select {
				case <-run.Serving():
					t.Fatal("premature serving")
				default:
				}
				if _, err := f.server.StartManaged(context.Background(), opts); !errors.Is(err, ErrManagedAlreadyStarted) {
					t.Fatalf("duplicate: %v", err)
				}
				if err := f.serve("127.0.0.1:0"); !errors.Is(err, ErrHostingMode) {
					t.Fatalf("mixed start: %v", err)
				}
				unblock()
				managedReady(t, run)
				if managedRequest(t, run.Address(), "/api/ok") != "ok" {
					t.Fatal("wrong body")
				}
				managedAwait(t, (<-requestContexts).Done())
				if err := run.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := run.Wait(context.Background()); err != nil {
					t.Fatal(err)
				}
				if run.State() != ManagedStopped {
					t.Fatal(run.State())
				}
				if _, err := f.server.StartManaged(context.Background(), opts); !errors.Is(err, ErrManagedAlreadyStarted) {
					t.Fatal(err)
				}
			})
			t.Run("pre-canceled-invalid-early-legacy", func(t *testing.T) {
				f := managedFixtureFor(t, major, FiberAdapterConfig{})
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if run, err := f.server.StartManaged(ctx, managedOptions()); run != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("%v %v", run, err)
				}
				if _, err := f.server.StartManaged(context.Background(), ManagedOptions{}); !errors.Is(err, ErrManagedOptions) {
					t.Fatal(err)
				}
				if err := f.shutdown(context.Background()); !errors.Is(err, ErrHostingMode) {
					t.Fatal(err)
				}
				run := managedStart(t, f, context.Background(), managedOptions())
				managedReady(t, run)
			})
			t.Run("raw-mixing", func(t *testing.T) {
				f := managedFixtureFor(t, major, FiberAdapterConfig{})
				f.raw()
				if _, err := f.server.StartManaged(context.Background(), managedOptions()); !errors.Is(err, ErrHostingMode) {
					t.Fatal(err)
				}
			})
			for _, where := range []string{"on-bound", "native-hook"} {
				t.Run("blocked-"+where, func(t *testing.T) {
					f := managedFixtureFor(t, major, FiberAdapterConfig{})
					entered, release := make(chan struct{}), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					block := func() error { close(entered); <-release; return nil }
					opts := managedOptions()
					opts.DrainTimeout = 50 * time.Millisecond
					if where == "on-bound" {
						opts.OnBound = func(context.Context, string) error { return block() }
					} else {
						f.onListen(block)
					}
					run := managedStart(t, f, context.Background(), opts)
					defer unblock()
					managedAwait(t, entered)
					err := run.Stop(context.Background())
					if !errors.Is(err, ErrManagedDrainTimeout) || !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal(err)
					}
					select {
					case <-run.Done():
						t.Fatal("joined blocked callback")
					default:
					}
					conn, err := net.DialTimeout("tcp", run.Address(), 100*time.Millisecond)
					if err == nil {
						conn.Close()
						t.Fatal("listener still open")
					}
					unblock()
					managedAwait(t, run.Done())
					select {
					case <-run.Serving():
						t.Fatal("late serving")
					default:
					}
				})
			}
			t.Run("100-controlled-stop-interleavings", func(t *testing.T) {
				for i := 0; i < 100; i++ {
					func() {
						f := managedFixtureFor(t, major, FiberAdapterConfig{})
						entered, release := make(chan struct{}), make(chan struct{})
						var once sync.Once
						unblock := func() { once.Do(func() { close(release) }) }
						opts := managedOptions()
						opts.OnBound = func(context.Context, string) error { close(entered); <-release; return nil }
						run := managedStart(t, f, context.Background(), opts)
						defer unblock()
						managedAwait(t, entered)
						canceled, cancel := context.WithCancel(context.Background())
						cancel()
						if err := run.Stop(canceled); !errors.Is(err, context.Canceled) {
							t.Fatal(err)
						}
						unblock()
						managedAwait(t, run.Done())
						if err := run.Wait(context.Background()); err != nil {
							t.Fatal(err)
						}
						select {
						case <-run.Serving():
							t.Fatal("late serving")
						default:
						}
					}()
				}
			})
			for _, kind := range []string{"endpoint", "same-path", "middleware", "mount", "hook"} {
				t.Run("reject-raw-"+kind, func(t *testing.T) {
					f := managedFixtureFor(t, major, FiberAdapterConfig{PreserveRegistrationOrder: true})
					f.get("/ok", func(c Context) error { return c.SendString("ok") })
					switch kind {
					case "endpoint":
						f.nativeGet("/raw")
					case "same-path":
						f.nativeGet("/api/ok")
					case "middleware":
						f.nativeUse()
					case "mount":
						f.nativeMount()
					case "hook":
						f.onListen(func() error { f.nativeGet("/raw"); return nil })
					}
					run := managedStart(t, f, context.Background(), managedOptions())
					managedAwait(t, run.Done())
					if err := run.Wait(context.Background()); !errors.Is(err, ErrManagedNativeRegistration) {
						t.Fatal(err)
					}
					if kind != "hook" {
						select {
						case <-run.Bound():
							t.Fatal("bound unaccounted routes")
						default:
						}
					}
					select {
					case <-run.Serving():
						t.Fatal("served unaccounted routes")
					default:
					}
				})
			}
			t.Run("cooperative-parent-drain", func(t *testing.T) {
				f := managedFixtureFor(t, major, FiberAdapterConfig{})
				entered, release := make(chan struct{}), make(chan struct{})
				var reqCtx context.Context
				f.get("/wait", func(c Context) error { reqCtx = c.Context(); close(entered); <-release; return c.SendString("done") })
				parent, cancel := context.WithCancelCause(context.Background())
				defer cancel(nil)
				run := managedStart(t, f, parent, managedOptions())
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				managedReady(t, run)
				response := make(chan error, 1)
				go func() {
					client := http.Client{Timeout: 2 * time.Second}
					res, err := client.Get("http://" + run.Address() + "/api/wait")
					if err == nil {
						_, err = io.ReadAll(res.Body)
						res.Body.Close()
					}
					response <- err
				}()
				managedAwait(t, entered)
				cause := errors.New("parent stopped")
				cancel(cause)
				managedAwait(t, run.Stopping())
				if reqCtx.Err() != nil {
					t.Fatal("drain context canceled prematurely")
				}
				unblock()
				if err := <-response; err != nil {
					t.Fatal(err)
				}
				managedAwait(t, run.Done())
				if err := run.Wait(context.Background()); !errors.Is(err, cause) {
					t.Fatal(err)
				}
			})
			t.Run("deadline-closes-connection-before-handler-joins", func(t *testing.T) {
				f := managedFixtureFor(t, major, FiberAdapterConfig{})
				entered, release := make(chan struct{}), make(chan struct{})
				var requestCtx context.Context
				f.get("/wait", func(c Context) error {
					requestCtx = c.Context()
					close(entered)
					<-release
					return c.SendString("late")
				})
				opts := managedOptions()
				opts.DrainTimeout = 60 * time.Millisecond
				run := managedStart(t, f, context.Background(), opts)
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				managedReady(t, run)
				conn, err := net.Dial("tcp", run.Address())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				fmt.Fprint(conn, "GET /api/wait HTTP/1.1\r\nHost: localhost\r\n\r\n")
				managedAwait(t, entered)
				if err := run.Stop(context.Background()); !errors.Is(err, ErrManagedDrainTimeout) {
					t.Fatal(err)
				}
				var b [1]byte
				if _, err := conn.Read(b[:]); err == nil {
					t.Fatal("active connection remained open")
				} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
					t.Fatal("connection only hit client deadline")
				}
				managedAwait(t, requestCtx.Done())
				select {
				case <-run.Done():
					t.Fatal("handler not joined")
				default:
				}
				unblock()
				managedAwait(t, run.Done())
			})
			t.Run("keepalive-admission-and-concurrent-stop", func(t *testing.T) {
				f := managedFixtureFor(t, major, FiberAdapterConfig{})
				f.get("/ok", func(c Context) error { return c.SendString("ok") })
				run := managedStart(t, f, context.Background(), managedOptions())
				managedReady(t, run)
				conn, err := net.Dial("tcp", run.Address())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				reader := bufio.NewReader(conn)
				fmt.Fprint(conn, "GET /api/ok HTTP/1.1\r\nHost: localhost\r\n\r\n")
				res, err := http.ReadResponse(reader, nil)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
				errs := make(chan error, 8)
				for range 8 {
					go func() { errs <- run.Stop(context.Background()) }()
				}
				managedAwait(t, run.Stopping())
				fmt.Fprint(conn, "GET /api/ok HTTP/1.1\r\nHost: localhost\r\n\r\n")
				res, err = http.ReadResponse(reader, nil)
				if err == nil {
					res.Body.Close()
					if res.StatusCode != 503 {
						t.Fatalf("admitted after stop: %d", res.StatusCode)
					}
				}
				for range 8 {
					if err := <-errs; err != nil {
						t.Fatal(err)
					}
				}
				managedAwait(t, run.Done())
			})
			for _, kind := range []string{"bind", "on-bound", "on-shutdown", "native-panic", "before-serve"} {
				if kind == "before-serve" && major == "v2" {
					continue
				}
				t.Run("error-"+kind, func(t *testing.T) {
					f := managedFixtureFor(t, major, FiberAdapterConfig{})
					opts := managedOptions()
					cause := errors.New("sentinel")
					want := cause
					switch kind {
					case "bind":
						opts.Address = "bad-address"
						want = nil
					case "on-bound":
						opts.OnBound = func(context.Context, string) error { return cause }
					case "on-shutdown":
						opts.OnShutdown = func(context.Context) error { return cause }
					case "native-panic":
						f.onListen(func() error { panic(cause) })
					case "before-serve":
						f.beforeServe(func() error { return cause })
					}
					run := managedStart(t, f, context.Background(), opts)
					if kind == "on-shutdown" {
						managedReady(t, run)
						_ = run.Stop(context.Background())
					}
					managedAwait(t, run.Done())
					err := run.Wait(context.Background())
					if err == nil || (want != nil && !errors.Is(err, want)) {
						t.Fatal(err)
					}
					if kind == "native-panic" {
						var pe *ManagedPanicError
						if !errors.As(err, &pe) {
							t.Fatalf("not typed panic: %v", err)
						}
					}
				})
			}
		})
	}
}

func TestStrictInitializationFailureSticky(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			var initialize func() error
			var init func()
			var serve func(string) error
			cfg := FiberAdapterConfig{StrictRoutes: true, NamedRoutePolicy: NamedRouteCollisionPolicyError}
			handler := func(c Context) error { return c.SendString("ok") }
			if major == "v2" {
				a := NewFiberAdapterWithConfig(cfg).(*FiberAdapter)
				a.Router().Get("/users/:id", handler).SetName("show")
				a.Router().Get("/members/:id", handler).SetName("show")
				initialize, init, serve = a.Initialize, a.Init, a.Serve
			} else {
				a := NewFiberV3AdapterWithConfig(FiberV3AdapterConfig{FiberAdapterConfig: cfg}).(*FiberV3Adapter)
				a.Router().Get("/users/:id", handler).SetName("show")
				a.Router().Get("/members/:id", handler).SetName("show")
				initialize, init, serve = a.Initialize, a.Init, a.Serve
			}
			var recovered any
			func() { defer func() { recovered = recover() }(); init() }()
			if recovered == nil {
				t.Fatal("missing legacy panic")
			}
			first := initialize()
			if first == nil || !strings.Contains(first.Error(), "ROUTE_NAME_CONFLICT") {
				t.Fatal(first)
			}
			errs := make(chan error, 20)
			for range 10 {
				go func() { errs <- initialize() }()
				go func() { errs <- serve("bad-address") }()
			}
			for range 20 {
				if err := <-errs; err != first {
					t.Fatalf("sticky identity lost: %v", err)
				}
			}
		})
	}
}
func TestManagedStrictNilHandler(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := managedFixtureFor(t, major, FiberAdapterConfig{StrictRoutes: true})
			f.get("/bad", nil)
			run := managedStart(t, f, context.Background(), managedOptions())
			managedAwait(t, run.Done())
			if err := run.Wait(context.Background()); err == nil || !strings.Contains(err.Error(), "nil handler") {
				t.Fatal(err)
			}
			select {
			case <-run.Bound():
				t.Fatal("invalid route bound")
			default:
			}
		})
	}
}

func TestManagedCancellationBoundaries(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := managedFixtureFor(t, major, FiberAdapterConfig{})
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var registrar StartupValidatorRegistrar
			switch a := f.server.(type) {
			case *FiberAdapter:
				registrar = a.Router().(StartupValidatorRegistrar)
			case *FiberV3Adapter:
				registrar = a.Router().(StartupValidatorRegistrar)
			}
			if err := registrar.RegisterStartupValidator("blocked", func() error { close(entered); <-release; return nil }); err != nil {
				t.Fatal(err)
			}
			opts := managedOptions()
			opts.DrainTimeout = 40 * time.Millisecond
			run := managedStart(t, f, context.Background(), opts)
			defer unblock()
			managedAwait(t, entered)
			if err := run.Stop(context.Background()); !errors.Is(err, ErrManagedDrainTimeout) {
				t.Fatal(err)
			}
			unblock()
			managedAwait(t, run.Done())
			select {
			case <-run.Bound():
				t.Fatal("bound after initialization cancellation")
			default:
			}
		})
	}
	t.Run("listener-acquired-after-cancel", func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		opts := managedOptions()
		opts.DrainTimeout = 40 * time.Millisecond
		driver := managedDriver{initialize: func() error { return nil }, validate: func() error { return nil }, prepare: func() *fasthttp.Server { return &fasthttp.Server{Handler: func(*fasthttp.RequestCtx) {}} }, listen: func(context.Context, string, string) (net.Listener, error) { close(entered); <-release; return ln, nil }, serve: func(net.Listener) error { t.Error("late serve"); return nil }}
		var hosting fiberHosting
		run, err := hosting.startManaged(context.Background(), opts, driver)
		if err != nil {
			t.Fatal(err)
		}
		managedAwait(t, entered)
		if err := run.Stop(context.Background()); !errors.Is(err, ErrManagedDrainTimeout) {
			t.Fatal(err)
		}
		unblock()
		managedAwait(t, run.Done())
		select {
		case <-run.Bound():
			t.Fatal("late bound publication")
		default:
		}
		conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
		if err == nil {
			conn.Close()
			t.Fatal("late listener leaked")
		}
	})
}

func TestManagedStreamOwnership(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := managedFixtureFor(t, major, FiberAdapterConfig{})
			connected := make(chan WebSocketContext, 1)
			socketDone := make(chan struct{})
			cfg := DefaultWebSocketConfig()
			cfg.DisableReadDeadline = true
			socketHandler := func(c WebSocketContext) error {
				defer close(socketDone)
				connected <- c
				_, _, _ = c.ReadMessage()
				return nil
			}
			switch a := f.server.(type) {
			case *FiberAdapter:
				a.Router().WebSocket("/ws", cfg, socketHandler)
			case *FiberV3Adapter:
				a.Router().WebSocket("/ws", cfg, socketHandler)
			}
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			streamStarted, streamDone := make(chan struct{}), make(chan struct{})
			stopStream := make(chan struct{})
			var stopOnce sync.Once
			stop := func() { stopOnce.Do(func() { close(stopStream); _ = writer.Close() }) }
			defer stop()
			f.get("/events", func(c Context) error {
				c.SetHeader("Content-Type", "text/event-stream")
				go func() {
					defer close(streamDone)
					_, _ = io.WriteString(writer, "data: ready\n\n")
					close(streamStarted)
					<-stopStream
				}()
				return c.SendStream(reader)
			})
			var socket WebSocketContext
			opts := managedOptions()
			opts.OnShutdown = func(ctx context.Context) error {
				stop()
				if socket != nil {
					_ = socket.Close()
				}
				select {
				case <-streamDone:
				case <-ctx.Done():
					return ctx.Err()
				}
				select {
				case <-socketDone:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			}
			run := managedStart(t, f, context.Background(), opts)
			managedReady(t, run)
			clientSocket, resp, err := (&websocket.Dialer{HandshakeTimeout: time.Second}).Dial("ws://"+run.Address()+"/ws", nil)
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer clientSocket.Close()
			select {
			case socket = <-connected:
			case <-time.After(time.Second):
				t.Fatal("websocket not connected")
			}
			streamConn, err := net.Dial("tcp", run.Address())
			if err != nil {
				t.Fatal(err)
			}
			defer streamConn.Close()
			streamConn.SetDeadline(time.Now().Add(2 * time.Second))
			fmt.Fprint(streamConn, "GET /api/events HTTP/1.1\r\nHost: localhost\r\n\r\n")
			managedAwait(t, streamStarted)
			if err := run.Stop(context.Background()); err != nil {
				r := run.(*managedRun)
				r.mu.Lock()
				t.Logf("connections=%d open=%d", len(r.connections), r.server.GetOpenConnectionsCount())
				r.mu.Unlock()
				for name, ch := range map[string]<-chan struct{}{"socket": socketDone, "stream": streamDone, "startup": r.startupDone, "watcher": r.watcherDone} {
					select {
					case <-ch:
						t.Log(name + " joined")
					default:
						t.Log(name + " pending")
					}
				}
				t.Fatal(err)
			}
			managedAwait(t, run.Done())
			managedAwait(t, streamDone)
			managedAwait(t, socketDone)
			wire, err := io.ReadAll(streamConn)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(wire), "data: ready") {
				t.Fatalf("SSE frame missing: %s", wire)
			}
			clientSocket.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := clientSocket.ReadMessage(); err == nil {
				t.Fatal("owned websocket still open")
			}
		})
	}
}

func TestManagedUnsupportedNativeSettings(t *testing.T) {
	for name, cfg := range map[string]f3.ListenConfig{"prefork": {EnablePrefork: true}, "tls": {TLSConfig: &tls.Config{}}, "unix": {ListenerNetwork: "unix"}, "competing-context": {GracefulContext: context.Background()}, "ignored-listener-callback": {ListenerAddrFunc: func(net.Addr) {}}} {
		t.Run(name, func(t *testing.T) {
			s := NewFiberV3AdapterWithConfig(FiberV3AdapterConfig{ListenConfig: cfg}).(ManagedServer)
			if run, err := s.StartManaged(context.Background(), managedOptions()); run != nil || !errors.Is(err, ErrManagedUnsupported) {
				t.Fatalf("%v %v", run, err)
			}
		})
	}
	s := NewFiberAdapter(func(*f2.App) *f2.App { return f2.New(f2.Config{Prefork: true}) }).(ManagedServer)
	if _, err := s.StartManaged(context.Background(), managedOptions()); !errors.Is(err, ErrManagedUnsupported) {
		t.Fatal(err)
	}
}

func TestManagedShutdownCallbacks(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := managedFixtureFor(t, major, FiberAdapterConfig{})
			opts := managedOptions()
			opts.DrainTimeout = 60 * time.Millisecond
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			late := errors.New("late callback failure")
			opts.OnShutdown = func(context.Context) error { close(entered); <-release; return late }
			run := managedStart(t, f, context.Background(), opts)
			defer unblock()
			managedReady(t, run)
			caller, cancel := context.WithCancel(context.Background())
			cancel()
			if err := run.Stop(caller); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			managedAwait(t, entered)
			if err := run.Stop(context.Background()); !errors.Is(err, ErrManagedDrainTimeout) {
				t.Fatal(err)
			}
			select {
			case <-run.Done():
				t.Fatal("premature completion")
			default:
			}
			unblock()
			managedAwait(t, run.Done())
			if err := run.Wait(context.Background()); !errors.Is(err, late) || !errors.Is(err, ErrManagedDrainTimeout) {
				t.Fatal(err)
			}
		})
	}
}
func TestManagedNativeHandlerMutationRejected(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := managedFixtureFor(t, major, FiberAdapterConfig{})
			f.onListen(func() error {
				switch a := f.server.(type) {
				case *FiberAdapter:
					a.app.Server().Handler = func(*fasthttp.RequestCtx) {}
				case *FiberV3Adapter:
					a.app.Server().Handler = func(*fasthttp.RequestCtx) {}
				}
				return nil
			})
			run := managedStart(t, f, context.Background(), managedOptions())
			managedAwait(t, run.Done())
			if err := run.Wait(context.Background()); !errors.Is(err, ErrManagedUnsupported) {
				t.Fatal(err)
			}
			select {
			case <-run.Serving():
				t.Fatal("overwritten handler admitted")
			default:
			}
		})
	}
}
func TestManagedLegacyEarlyStopRejected(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := managedFixtureFor(t, major, FiberAdapterConfig{})
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			f.onListen(func() error { close(entered); <-release; return nil })
			f.get("/ok", func(c Context) error { return c.SendString("ok") })
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := ln.Addr().String()
			ln.Close()
			done := make(chan error, 1)
			go func() { done <- f.serve(address) }()
			t.Cleanup(func() {
				unblock()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = f.shutdown(ctx)
				select {
				case <-done:
				case <-ctx.Done():
					t.Error("legacy serve did not join")
				}
			})
			managedAwait(t, entered)
			if err := f.shutdown(context.Background()); !errors.Is(err, ErrHostingMode) {
				t.Fatal(err)
			}
			if _, err := f.server.StartManaged(context.Background(), managedOptions()); !errors.Is(err, ErrHostingMode) {
				t.Fatal(err)
			}
			unblock()
			managedRequest(t, address, "/api/ok")
		})
	}
}
