package router

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/valyala/fasthttp"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f2 "github.com/gofiber/fiber/v2"
	f3 "github.com/gofiber/fiber/v3"
)

func TestBoundedIngressRegressionReplacementCancellation(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, mode := range []string{"cancel", "deadline"} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/enabled=%t", major, mode, enabled), func(t *testing.T) {
					f := newIngressFixture(major, false)
					f.register("GET", "/", func(c Context) error {
						originalID, _ := IngressRequestIDFromContext(c.Context())
						var replacement context.Context
						var cancel context.CancelFunc
						if mode == "cancel" {
							replacement, cancel = context.WithCancel(c.Context())
						} else {
							replacement, cancel = context.WithDeadline(c.Context(), time.Now().Add(-time.Second))
						}
						defer cancel()
						if mode == "cancel" {
							cancel()
						}
						wantErr := replacement.Err()
						wantDeadline, wantHasDeadline := replacement.Deadline()
						c.SetContext(replacement)
						gotID, _ := IngressRequestIDFromContext(c.Context())
						gotDeadline, gotHasDeadline := c.Context().Deadline()
						if c.Context().Err() != wantErr || gotHasDeadline != wantHasDeadline || gotHasDeadline && !gotDeadline.Equal(wantDeadline) {
							t.Errorf("replacement cancellation lost: want err=%v deadline=%v; got err=%v deadline=%v cause=%v", wantErr, wantHasDeadline, c.Context().Err(), gotHasDeadline, context.Cause(c.Context()))
						}
						if gotID != originalID {
							t.Error("identity lost")
						}
						return c.SendString("ok")
					})
					if enabled {
						ingressActivate(t, f, BoundedIngressOptions{})
					} else if err := f.initialize(); err != nil {
						t.Fatal(err)
					}
					ingressTransport(t, f, "test")("GET", "/")
				})
			}
		}
	}
}

func TestBoundedIngressRegressionHeaderCase(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, disabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/disabled=%t", major, disabled), func(t *testing.T) {
				var initialize func() error
				var request func(*http.Request) (*http.Response, error)
				handler := func(c Context) error {
					id, _ := IngressRequestIDFromContext(c.Context())
					lower := c.Header("x-request-id")
					if lower == "client-controlled" {
						t.Errorf("client correlation header survived: lower=%q owned=%q", lower, id)
					}
					c.SetHeader("x-request-id", "downstream-value")
					return c.SendString(id)
				}
				if major == "v2" {
					app := f2.New(f2.Config{DisableHeaderNormalizing: disabled, DisableStartupMessage: true})
					a := NewFiberAdapter(func(*f2.App) *f2.App { return app }).(*FiberAdapter)
					a.Router().Get("/", handler)
					if err := a.ConfigureBoundedIngress(BoundedIngressOptions{}); err != nil {
						t.Fatal(err)
					}
					initialize = a.Initialize
					request = func(r *http.Request) (*http.Response, error) { return app.Test(r) }
				} else {
					app := f3.New(f3.Config{DisableHeaderNormalizing: disabled})
					a := NewFiberV3Adapter(func(*f3.App) *f3.App { return app }).(*FiberV3Adapter)
					a.Router().Get("/", handler)
					if err := a.ConfigureBoundedIngress(BoundedIngressOptions{}); err != nil {
						t.Fatal(err)
					}
					initialize = a.Initialize
					request = func(r *http.Request) (*http.Response, error) { return app.Test(r) }
				}
				if err := initialize(); err != nil {
					t.Fatal(err)
				}
				req, _ := http.NewRequest("GET", "http://example.test/", nil)
				req.Header["x-request-id"] = []string{"client-controlled"}
				res, err := request(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)
				ids := res.Header.Values(XRequestID)
				t.Logf("response IDs=%q owned=%q", ids, body)
				if len(ids) != 1 || ids[0] != string(body) {
					t.Errorf("ambiguous correlation header: %q, owned=%q", ids, body)
				}
			})
		}
	}
}

func TestBoundedIngressRegressionGETOnly(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, getOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/getonly=%t", major, getOnly), func(t *testing.T) {
				var server ManagedServer
				var config BoundedIngressServer
				var initialize StartupInitializer
				if major == "v2" {
					app := f2.New(f2.Config{GETOnly: getOnly, DisableStartupMessage: true})
					a := NewFiberAdapter(func(*f2.App) *f2.App { return app }).(*FiberAdapter)
					server = a
					config = a
					initialize = a
				} else {
					app := f3.New(f3.Config{GETOnly: getOnly})
					a := NewFiberV3AdapterWithConfig(FiberV3AdapterConfig{ListenConfig: f3.ListenConfig{DisableStartupMessage: true}}, func(*f3.App) *f3.App { return app }).(*FiberV3Adapter)
					server = a
					config = a
					initialize = a
				}
				var calls, events atomic.Int32
				options := BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
					calls.Add(1)
					return UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: UnknownMethodRejection{Status: 429, Code: "CUSTOM", Message: "custom rejection"}}, nil
				})}, Observer: func(IngressCancellation, IngressEvent) error { events.Add(1); return nil }}
				if err := config.ConfigureBoundedIngress(options); getOnly {
					requireIngressGETOnlyError(t, err)
					requireIngressGETOnlyError(t, initialize.Initialize())
					return
				} else if err != nil {
					t.Fatal(err)
				}
				if err := initialize.Initialize(); err != nil {
					t.Fatal(err)
				}
				run := managedStart(t, managedFixture{server: server}, context.Background(), managedOptions())
				managedReady(t, run)
				response, body := ingressWire(t, run.Address(), "X-VALID-UNKNOWN", "/missing")
				t.Logf("status=%d id=%q body=%q strategy=%d events=%d", response.StatusCode, response.Header.Get(XRequestID), body, calls.Load(), events.Load())
				if calls.Load() != 1 || events.Load() != 1 || response.StatusCode != 429 || response.Header.Get(XRequestID) == "" {
					t.Error("accepted GETOnly configuration bypasses bounded ingress on valid request")
				}
			})
		}
	}
}

func requireIngressGETOnlyError(t *testing.T, err error) {
	t.Helper()
	var typed *IngressConfigurationError
	if !errors.Is(err, ErrIngressConfiguration) || !errors.As(err, &typed) || typed.Field != "native.get_only" || typed.Rule != "unsupported" {
		t.Fatalf("GETOnly must fail typed validation: %v", err)
	}
}

func TestBoundedIngressGETOnlyLifecycle(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, stage := range []string{"preflight", "configure", "initialize", "prepare", "bound", "native-hook", "no-option"} {
			t.Run(major+"/"+stage, func(t *testing.T) {
				f := newIngressFixture(major, false)
				validate := f.managed.(BoundedIngressValidator).ValidateBoundedIngress
				if stage == "preflight" || stage == "configure" || stage == "no-option" {
					f.server.GetOnly = true
				}
				if stage == "preflight" {
					requireIngressGETOnlyError(t, validate(BoundedIngressOptions{}))
					f.server.GetOnly = false
				}
				if stage == "configure" {
					requireIngressGETOnlyError(t, f.configure(BoundedIngressOptions{}))
					// A successful retry before finalization clears the sticky error.
					f.server.GetOnly = false
				}
				if stage != "no-option" {
					if err := f.configure(BoundedIngressOptions{}); err != nil {
						t.Fatal(err)
					}
				}
				if stage == "initialize" {
					f.server.GetOnly = true
				}
				if stage == "prepare" {
					f.register("GET", "/", func(c Context) error { return nil })
					switch a := f.managed.(type) {
					case *FiberAdapter:
						a.app.Hooks().OnRoute(func(f2.Route) error { f.server.GetOnly = true; return nil })
					case *FiberV3Adapter:
						a.app.Hooks().OnRoute(func(f3.Route) error { f.server.GetOnly = true; return nil })
					}
				}
				if stage == "native-hook" {
					switch a := f.managed.(type) {
					case *FiberAdapter:
						a.app.Hooks().OnListen(func(f2.ListenData) error { f.server.GetOnly = true; return nil })
					case *FiberV3Adapter:
						a.listenConfig.BeforeServeFunc = func(*f3.App) error { f.server.GetOnly = true; return nil }
					}
				}
				opts := managedOptions()
				if stage == "bound" {
					opts.OnBound = func(context.Context, string) error { f.server.GetOnly = true; return nil }
				}
				run := managedStart(t, managedFixture{server: f.managed}, context.Background(), opts)
				switch stage {
				case "initialize", "prepare", "bound", "native-hook":
					managedAwait(t, run.Done())
					requireIngressGETOnlyError(t, run.Wait(context.Background()))
					select {
					case <-run.Serving():
						t.Fatal("invalid native mode served")
					default:
					}
					if stage == "initialize" || stage == "prepare" {
						select {
						case <-run.Bound():
							t.Fatal("invalid native mode bound")
						default:
						}
					}
				default:
					managedReady(t, run)
					if stage == "no-option" {
						res, _ := ingressWire(t, run.Address(), "X-UNKNOWN", "/")
						if res.StatusCode != 405 || res.Header.Get(XRequestID) != "" {
							t.Fatalf("changed unopted native GETOnly: %v", res)
						}
					}
				}
			})
		}
	}
}

func TestBoundedIngressCancellationComposition(t *testing.T) {
	type key struct{}
	cause := errors.New("application cancellation")
	for _, mode := range []string{"child", "independent", "without-cancel", "expired", "live-deadline", "owned-deadline"} {
		for _, winner := range []string{"application", "owned"} {
			t.Run(mode+"/"+winner, func(t *testing.T) {
				owned, stopOwned := context.WithCancel(context.Background())
				defer stopOwned()
				var owner context.Context = owned
				if mode == "owned-deadline" {
					var cancel context.CancelFunc
					owner, cancel = context.WithDeadline(owner, time.Now().Add(-time.Second))
					defer cancel()
				}
				original := newIngressContext(owner, "owned-id")
				var base context.Context = context.Background()
				if mode == "child" {
					base = original
				}
				if mode == "without-cancel" {
					base = context.WithoutCancel(original)
				}
				app, cancelApp := context.WithCancelCause(context.WithValue(base, key{}, "trusted"))
				defer cancelApp(nil)
				var replacement context.Context = app
				if mode == "expired" || mode == "live-deadline" {
					deadline := time.Now().Add(time.Hour)
					if mode == "expired" {
						deadline = time.Now().Add(-time.Second)
					}
					var cancel context.CancelFunc
					replacement, cancel = context.WithDeadline(app, deadline)
					defer cancel()
				}
				got := preserveIngressContext(original, replacement)
				if got.Value(key{}) != "trusted" {
					t.Fatal("lost replacement values")
				}
				if id, _ := IngressRequestIDFromContext(got); id != "owned-id" {
					t.Fatal("lost ID")
				}
				wantDeadline, wantOK := replacement.Deadline()
				if mode == "owned-deadline" {
					wantDeadline, wantOK = owner.Deadline()
				}
				if d, ok := got.Deadline(); ok != wantOK || ok && !d.Equal(wantDeadline) {
					t.Fatalf("deadline mismatch %v %t", d, ok)
				}
				child, cancelChild := context.WithCancel(got)
				defer cancelChild()
				wantErr, wantCause := context.Canceled, error(cause)
				if mode == "expired" || mode == "owned-deadline" {
					wantErr, wantCause = context.DeadlineExceeded, context.DeadlineExceeded
				} else if winner == "owned" {
					stopOwned()
					wantCause = context.Canceled
				} else {
					cancelApp(cause)
				}
				managedAwait(t, got.Done())
				managedAwait(t, child.Done())
				if got.Err() != wantErr || child.Err() != wantErr || context.Cause(got) != wantCause || context.Cause(child) != wantCause {
					t.Fatalf("lost cancellation: got=%v/%v child=%v/%v", got.Err(), context.Cause(got), child.Err(), context.Cause(child))
				}
			})
		}
	}
	// Simultaneous cancellation and registration must detach both sources safely.
	for n := 0; n < 100; n++ {
		owned, a := context.WithCancel(context.Background())
		app, b := context.WithCancel(context.Background())
		got := preserveIngressContext(newIngressContext(owned, "id"), app)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); a() }()
		go func() { defer wg.Done(); b() }()
		wg.Wait()
		managedAwait(t, got.Done())
		if got.Err() != context.Canceled {
			t.Fatal(got.Err())
		}
	}
}

func ingressHeaderFixture(major string, disabled bool) ingressFixture {
	if major == "v2" {
		app := f2.New(f2.Config{DisableHeaderNormalizing: disabled, DisableStartupMessage: true})
		a := NewFiberAdapter(func(*f2.App) *f2.App { return app }).(*FiberAdapter)
		return ingressFixture{configure: a.ConfigureBoundedIngress, initialize: a.Initialize, test: func(r *http.Request) (*http.Response, error) { return app.Test(r, 3000) }, register: func(m, p string, h HandlerFunc) { a.Router().Handle(HTTPMethod(m), p, h) }, use: func(m MiddlewareFunc) { a.Router().Use(m) }, managed: a, server: app.Server()}
	}
	app := f3.New(f3.Config{DisableHeaderNormalizing: disabled})
	a := NewFiberV3AdapterWithConfig(FiberV3AdapterConfig{ListenConfig: f3.ListenConfig{DisableStartupMessage: true}}, func(*f3.App) *f3.App { return app }).(*FiberV3Adapter)
	return ingressFixture{configure: a.ConfigureBoundedIngress, initialize: a.Initialize, test: func(r *http.Request) (*http.Response, error) {
		return app.Test(r, f3.TestConfig{Timeout: 3 * time.Second})
	}, register: func(m, p string, h HandlerFunc) { a.Router().Handle(HTTPMethod(m), p, h) }, use: func(m MiddlewareFunc) { a.Router().Use(m) }, managed: a, server: app.Server()}
}

func TestBoundedIngressHeaderOwnership(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, disabled := range []bool{false, true} {
			for _, transport := range []string{"test", "socket"} {
				for _, kind := range []string{"router", "native", "writer", "stream", "reject", "custom", "native-unknown"} {
					t.Run(fmt.Sprintf("%s/disabled=%t/%s/%s", major, disabled, transport, kind), func(t *testing.T) {
						f := ingressHeaderFixture(major, disabled)
						events := make(chan IngressEvent, 1)
						f.register("GET", "/", func(c Context) error {
							var native *fasthttp.RequestCtx
							switch c := c.(type) {
							case *fiberContext:
								native = c.ctx.Context()
							case *fiberV3Context:
								native = c.ctx.RequestCtx()
							}
							id, _ := IngressRequestIDFromContext(c.Context())
							count := 0
							native.Request.Header.VisitAll(func(k, v []byte) {
								if strings.EqualFold(string(k), XRequestID) {
									count++
									if string(v) != id {
										t.Errorf("untrusted request ID: %q", v)
									}
								}
							})
							if count != 1 || string(native.Request.Header.Peek("x-unrelated")) != "client-value" && disabled {
								t.Errorf("request fields count=%d", count)
							}
							native.Response.Header.Set("x-unrelated", "native-value")
							switch kind {
							case "router":
								c.SetHeader("x-request-id", "wrong")
								c.(interface{ AppendResponseHeader(string, string) Context }).AppendResponseHeader("X-REQUEST-ID", "wrong-again")
								return c.SendString(id)
							case "writer":
								w := c.(interface{ Response() http.ResponseWriter }).Response()
								w.Header()["x-request-id"] = []string{"wrong", "wrong-again"}
								w.Header()["X-REQUEST-ID"] = []string{"wrong-third"}
								_, err := w.Write([]byte(id))
								return err
							case "stream":
								native.Response.Header.Set("x-request-id", "wrong")
								return c.SendStream(strings.NewReader(id))
							default:
								native.Response.Header.Set("x-request-id", "wrong")
								native.Response.Header.Add("X-REQUEST-ID", "wrong-again")
								return c.SendString(id)
							}
						})
						opts := BoundedIngressOptions{Observer: func(_ IngressCancellation, e IngressEvent) error { events <- e; return nil }}
						method := "GET"
						switch kind {
						case "reject":
							method = "X-UNKNOWN"
							opts.UnknownMethod.Policy = UnknownMethodReject
						case "custom":
							method = "X-UNKNOWN"
							opts.UnknownMethod = UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
								return UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: UnknownMethodRejection{Status: 429, Code: "WAIT", Message: "wait"}}, nil
							})}
						case "native-unknown":
							method = "X-UNKNOWN"
						}
						ingressActivate(t, f, opts)
						var res *http.Response
						var err error
						var conn net.Conn
						if transport == "test" {
							req, _ := http.NewRequest(method, "http://example.test/", nil)
							req.Header["x-request-id"] = []string{"untrusted", "untrusted-duplicate"}
							req.Header["X-REQUEST-ID"] = []string{"untrusted-mixed"}
							req.Header["x-unrelated"] = []string{"client-value"}
							res, err = f.test(req)
						} else {
							run := managedStart(t, managedFixture{server: f.managed}, context.Background(), managedOptions())
							managedReady(t, run)
							conn, err = net.DialTimeout("tcp", run.Address(), time.Second)
							if err != nil {
								t.Fatal(err)
							}
							defer conn.Close()
							_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
							_, err = fmt.Fprintf(conn, "%s / HTTP/1.1\r\nHost: example.test\r\nx-request-id: untrusted\r\nx-request-id: duplicate\r\nX-REQUEST-ID: mixed\r\nx-unrelated: client-value\r\nConnection: close\r\n\r\n", method)
							if err != nil {
								t.Fatal(err)
							}
							res, err = http.ReadResponse(bufio.NewReader(conn), nil)
						}
						if err != nil {
							t.Fatal(err)
						}
						defer res.Body.Close()
						body, err := io.ReadAll(res.Body)
						if err != nil {
							t.Fatal(err)
						}
						event := <-events
						ids := res.Header.Values(XRequestID)
						if len(ids) != 1 || ids[0] != event.RequestID || ids[0] == "" {
							t.Fatalf("ambiguous response ID %q, event %q", ids, event.RequestID)
						}
						if method == "GET" && (string(body) != ids[0] || res.Header.Get("X-Unrelated") != "native-value") {
							t.Fatalf("response altered: %q %v", body, res.Header)
						}
					})
				}
			}
		}
	}
}

func TestBoundedIngressHeaderWebSocket(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := ingressHeaderFixture(major, true)
			ended := make(chan struct{})
			idsFromHandler := make(chan string, 1)
			cfg := DefaultWebSocketConfig()
			cfg.DisableReadDeadline = true
			cfg.OnPreUpgrade = func(c Context) (UpgradeData, error) {
				switch c := c.(type) {
				case *fiberContext:
					c.ctx.Response().Header.Set("x-request-id", "wrong")
				case *fiberV3Context:
					c.ctx.Response().Header.Set("x-request-id", "wrong")
				}
				return nil, nil
			}
			handler := func(c WebSocketContext) error {
				defer close(ended)
				id, _ := IngressRequestIDFromContext(c.Context())
				idsFromHandler <- id
				return c.WriteMessage(1, []byte(id))
			}
			switch a := f.managed.(type) {
			case *FiberAdapter:
				a.Router().WebSocket("/ws", cfg, handler)
			case *FiberV3Adapter:
				a.Router().WebSocket("/ws", cfg, handler)
			}
			ingressActivate(t, f, BoundedIngressOptions{})
			run := managedStart(t, managedFixture{server: f.managed}, context.Background(), managedOptions())
			managedReady(t, run)
			// With native normalization disabled, use the native upgrader's
			// documented header spelling; ingress owns only X-Request-ID.
			conn, err := net.DialTimeout("tcp", run.Address(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			_, err = fmt.Fprint(conn, "GET /ws HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-Websocket-Version: 13\r\nSec-Websocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nx-request-id: client\r\nX-REQUEST-ID: other\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			res, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != 101 {
				body, _ := io.ReadAll(res.Body)
				t.Fatalf("handshake status=%d body=%s", res.StatusCode, body)
			}
			var id string
			select {
			case id = <-idsFromHandler:
			case <-time.After(time.Second):
				t.Fatal("missing WebSocket handler")
			}
			ids := res.Header.Values(XRequestID)
			if len(ids) != 1 || ids[0] != id || id == "" {
				t.Fatalf("upgrade lost ID: %q %q", ids, id)
			}
			managedAwait(t, ended)
		})
	}
}

func TestBoundedIngressReplacementCallbackLifetime(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, mode := range []string{"router", "native-before", "native-unwind"} {
			t.Run(major+"/"+mode, func(t *testing.T) {
				f := newIngressFixture(major, false)
				var calls atomic.Int32
				expired := func(ctx context.Context) context.Context {
					v, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
					cancel()
					return v
				}
				f.use(func(next HandlerFunc) HandlerFunc {
					return func(c Context) error {
						err := next(c)
						if c.Context().Err() != context.DeadlineExceeded {
							t.Errorf("replacement deadline lost during unwind: %v", c.Context().Err())
						}
						return err
					}
				})
				if mode != "router" {
					if major == "v2" {
						f.use(MiddlewareFromFiber(func(c *f2.Ctx) error {
							if mode == "native-before" {
								c.SetUserContext(expired(c.UserContext()))
							}
							err := c.Next()
							if mode == "native-unwind" {
								c.SetUserContext(expired(c.UserContext()))
							}
							return err
						}))
					} else {
						f.use(MiddlewareFromFiberV3(func(c f3.Ctx) error {
							if mode == "native-before" {
								c.SetContext(expired(c.Context()))
							}
							err := c.Next()
							if mode == "native-unwind" {
								c.SetContext(expired(c.Context()))
							}
							return err
						}))
					}
				}
				f.register("GET", "/", func(c Context) error {
					if mode == "router" {
						c.SetContext(expired(c.Context()))
					}
					if mode != "native-unwind" && c.Context().Err() != context.DeadlineExceeded {
						t.Errorf("lost application deadline: %v", c.Context().Err())
					}
					return c.SendString("ok")
				})
				ingressActivate(t, f, BoundedIngressOptions{Observer: func(c IngressCancellation, _ IngressEvent) error {
					calls.Add(1)
					if c.Err() != nil {
						t.Errorf("application canceled observer: %v", c.Err())
					}
					return errors.New("diagnostic")
				}, Diagnostic: func(c IngressCancellation, _ IngressDiagnostic) {
					calls.Add(1)
					if c.Err() != nil {
						t.Errorf("application canceled diagnostic: %v", c.Err())
					}
				}})
				res, _ := ingressTransport(t, f, "socket")("GET", "/")
				if res.StatusCode != 200 || calls.Load() != 2 {
					t.Fatalf("callback delivery %d %d", res.StatusCode, calls.Load())
				}
			})
		}
	}
}

func TestBoundedIngressReplacementManagedDrain(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, mode := range []string{"independent", "without-cancel"} {
			t.Run(major+"/"+mode, func(t *testing.T) {
				f := newIngressFixture(major, false)
				entered := make(chan struct{})
				done := make(chan struct{})
				var retained context.Context
				f.register("GET", "/", func(c Context) error {
					replacement := context.Background()
					if mode == "without-cancel" {
						replacement = context.WithoutCancel(c.Context())
					}
					c.SetContext(replacement)
					retained = c.Context()
					close(entered)
					<-retained.Done()
					close(done)
					return nil
				})
				ingressActivate(t, f, BoundedIngressOptions{})
				opts := managedOptions()
				opts.DrainTimeout = 30 * time.Millisecond
				run := managedStart(t, managedFixture{server: f.managed}, context.Background(), opts)
				managedReady(t, run)
				requestDone := make(chan struct{})
				go func() {
					defer close(requestDone)
					client := http.Client{Timeout: time.Second}
					res, _ := client.Get("http://" + run.Address() + "/")
					if res != nil {
						res.Body.Close()
					}
				}()
				managedAwait(t, entered)
				if retained.Err() != nil {
					t.Fatal("request canceled before drain")
				}
				if err := run.Stop(context.Background()); !errors.Is(err, ErrManagedDrainTimeout) {
					t.Fatalf("drain result %v", err)
				}
				managedAwait(t, done)
				managedAwait(t, run.Done())
				managedAwait(t, requestDone)
				if retained.Err() != context.Canceled {
					t.Fatal(retained.Err())
				}
			})
		}
	}
}

func TestBoundedIngressHeaderConcurrentIsolation(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := ingressHeaderFixture(major, true)
			f.register("GET", "/", func(c Context) error {
				id, _ := IngressRequestIDFromContext(c.Context())
				c.SetHeader("x-request-id", "wrong")
				return c.SendString(id)
			})
			ingressActivate(t, f, BoundedIngressOptions{})
			run := managedStart(t, managedFixture{server: f.managed}, context.Background(), managedOptions())
			managedReady(t, run)
			transport := &http.Transport{}
			defer transport.CloseIdleConnections()
			client := http.Client{Transport: transport, Timeout: time.Second}
			var wg sync.WaitGroup
			var seen sync.Map
			for n := 0; n < 30; n++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					req, _ := http.NewRequest("GET", "http://"+run.Address()+"/", nil)
					req.Header["x-request-id"] = []string{"client"}
					req.Header["X-REQUEST-ID"] = []string{"other"}
					res, err := client.Do(req)
					if err != nil {
						t.Error(err)
						return
					}
					defer res.Body.Close()
					body, err := io.ReadAll(res.Body)
					if err != nil {
						t.Error(err)
						return
					}
					ids := res.Header.Values(XRequestID)
					if len(ids) != 1 || ids[0] != string(body) || ids[0] == "" {
						t.Errorf("lost ID: %q %q", ids, body)
						return
					}
					if _, loaded := seen.LoadOrStore(ids[0], true); loaded {
						t.Error("cross-request ID reuse")
					}
				}()
			}
			wg.Wait()
		})
	}
}

// Use the documented context.AfterFunc protocol to observe detachment without
// inspecting the standard library's private cancellation tree.
type ingressTrackedContext struct {
	context.Context
	active atomic.Int32
}

func (c *ingressTrackedContext) Value(any) any { return nil }
func (c *ingressTrackedContext) AfterFunc(f func()) func() bool {
	c.active.Add(1)
	var once sync.Once
	finish := func() { once.Do(func() { c.active.Add(-1) }) }
	stop := context.AfterFunc(c.Context, func() { finish(); f() })
	return func() bool {
		ok := stop()
		if ok {
			finish()
		}
		return ok
	}
}
func TestBoundedIngressCancellationDetaches(t *testing.T) {
	independent, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &ingressTrackedContext{Context: independent}
	for n := 0; n < 50; n++ {
		owner, end := context.WithCancel(context.Background())
		got := preserveIngressContext(newIngressContext(owner, "id"), source)
		if source.active.Load() != 1 {
			t.Fatal("missing cancellation registration")
		}
		end()
		managedAwait(t, got.Done())
		// Done publication precedes unregistering the callbacks. Bound the wait for
		// that internal cleanup without imposing ordering on application callbacks.
		deadline := time.Now().Add(time.Second)
		for source.active.Load() != 0 && time.Now().Before(deadline) {
			runtime.Gosched()
		}
		if source.active.Load() != 0 {
			t.Fatal("application parent retains completed request")
		}
	}
}

func TestBoundedIngressCancellationLiveDeadline(t *testing.T) {
	for _, winner := range []string{"owned", "application"} {
		t.Run(winner, func(t *testing.T) {
			cause := errors.New("deadline cause")
			timed, cancel := context.WithTimeoutCause(context.Background(), 20*time.Millisecond, cause)
			defer cancel()
			var owned, application context.Context = context.Background(), context.Background()
			if winner == "owned" {
				owned = timed
			} else {
				application = timed
			}
			joined := preserveIngressContext(newIngressContext(owned, "id"), application)
			child, stop := context.WithCancel(joined)
			defer stop()
			managedAwait(t, child.Done())
			if joined.Err() != context.DeadlineExceeded || child.Err() != context.DeadlineExceeded || context.Cause(child) != cause {
				t.Fatalf("deadline propagation: %v/%v child=%v/%v", joined.Err(), context.Cause(joined), child.Err(), context.Cause(child))
			}
		})
	}
}

func TestBoundedIngressCancellationAfterFunc(t *testing.T) {
	for n := 0; n < 100; n++ {
		owner, cancel := context.WithCancel(context.Background())
		joined := joinIngressCancellation(owner, context.Background()).(*ingressCancellationContext)
		done := joined.Done()
		called := make(chan struct{})
		stop := joined.AfterFunc(func() {
			defer close(called)
			select {
			case <-done:
			default:
				t.Error("callback ran before cancellation publication")
			}
			if context.Cause(joined) != context.Canceled {
				t.Error("cause not published")
			}
		})
		cancel()
		managedAwait(t, called)
		stop()
	}
}
