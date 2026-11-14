package router

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f2 "github.com/gofiber/fiber/v2"
	f3 "github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/valyala/fasthttp"
)

type ingressFixture struct {
	configure   func(BoundedIngressOptions) error
	initialize  func() error
	test        func(*http.Request) (*http.Response, error)
	register    func(string, string, HandlerFunc)
	use         func(MiddlewareFunc)
	managed     ManagedServer
	methods     []string
	runtime     func() *ingressRuntime
	server      *fasthttp.Server
	mutateRoute func()
}

func newIngressFixture(major string, customMethods bool) ingressFixture {
	methods := append([]string(nil), f2.DefaultMethods...)
	if customMethods {
		methods = append(methods, "X-KNOWN")
	}
	if major == "v2" {
		app := f2.New(f2.Config{RequestMethods: methods, DisableStartupMessage: true})
		a := NewFiberAdapter(func(*f2.App) *f2.App { return app }).(*FiberAdapter)
		return ingressFixture{configure: a.ConfigureBoundedIngress, initialize: a.Initialize, test: func(r *http.Request) (*http.Response, error) { return app.Test(r, 5000) }, register: func(m, p string, h HandlerFunc) { a.Router().Handle(HTTPMethod(m), p, h) }, use: func(m MiddlewareFunc) { a.Router().Use(m) }, managed: a, methods: app.Config().RequestMethods, runtime: func() *ingressRuntime { return a.ingress.runtime }, server: app.Server(), mutateRoute: func() { app.Get("/raw", func(c *f2.Ctx) error { return c.SendString("raw") }) }}
	}
	app := f3.New(f3.Config{RequestMethods: methods})
	a := NewFiberV3AdapterWithConfig(FiberV3AdapterConfig{ListenConfig: f3.ListenConfig{DisableStartupMessage: true}}, func(*f3.App) *f3.App { return app }).(*FiberV3Adapter)
	return ingressFixture{configure: a.ConfigureBoundedIngress, initialize: a.Initialize, test: func(r *http.Request) (*http.Response, error) {
		return app.Test(r, f3.TestConfig{Timeout: 5 * time.Second})
	}, register: func(m, p string, h HandlerFunc) { a.Router().Handle(HTTPMethod(m), p, h) }, use: func(m MiddlewareFunc) { a.Router().Use(m) }, managed: a, methods: app.Config().RequestMethods, runtime: func() *ingressRuntime { return a.ingress.runtime }, server: app.Server(), mutateRoute: func() { app.Get("/raw", func(c f3.Ctx) error { return c.SendString("raw") }) }}
}
func ingressActivate(t *testing.T, f ingressFixture, o BoundedIngressOptions) {
	t.Helper()
	if err := f.configure(o); err != nil {
		t.Fatal(err)
	}
	if err := f.initialize(); err != nil {
		t.Fatal(err)
	}
}
func ingressTransport(t *testing.T, f ingressFixture, transport string) func(string, string) (*http.Response, []byte) {
	t.Helper()
	request := f.test
	base := "http://example.test"
	if transport == "socket" {
		run := managedStart(t, managedFixture{server: f.managed}, context.Background(), managedOptions())
		managedReady(t, run)
		base = "http://" + run.Address()
		tr := &http.Transport{}
		t.Cleanup(tr.CloseIdleConnections)
		client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
		request = client.Do
	}
	return func(method, path string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(XRequestID, "untrusted-client-id")
		response, err := request(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return response, body
	}
}
func ingressEnvelope(t *testing.T, r *http.Response, body []byte, status int, code string) {
	t.Helper()
	var e IngressErrorEnvelope
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("%d %q: %v", r.StatusCode, body, err)
	}
	if r.StatusCode != status || e.Code != code || e.RequestID == "" || e.RequestID == "untrusted-client-id" || e.RequestID != r.Header.Get(XRequestID) || r.Header.Get("Allow") != "" || r.Header.Get("Upgrade") != "" || len(body) > IngressMaxEnvelopeBytes {
		t.Fatalf("response %d %#v %s", r.StatusCode, r.Header, body)
	}
}
func TestBoundedIngressRuntimePolicies(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, transport := range []string{"test", "socket"} {
			for _, policy := range []UnknownMethodPolicy{UnknownMethodNative, UnknownMethodReject, UnknownMethodCustom} {
				t.Run(fmt.Sprintf("%s/%s/%d", major, transport, policy), func(t *testing.T) {
					f := newIngressFixture(major, true)
					var eventsMu sync.Mutex
					var events []IngressEvent
					var facts []UnknownMethodRequest
					var factsMu sync.Mutex
					var calls atomic.Int32
					f.register("GET", "/healthz", func(c Context) error {
						calls.Add(1)
						id, _ := IngressRequestIDFromContext(c.Context())
						if id == "" || c.Header(XRequestID) != id {
							return fmt.Errorf("missing identity")
						}
						return c.SendString("ok")
					})
					f.register("HEAD", "/healthz", func(c Context) error { calls.Add(1); return c.SendString("head") })
					f.register("OPTIONS", "/healthz", func(c Context) error { calls.Add(1); return c.NoContent(204) })
					f.register("X-KNOWN", "/healthz", func(c Context) error { calls.Add(1); return c.SendString("custom-known") })
					opt := BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: policy}, Observer: func(_ IngressCancellation, e IngressEvent) error {
						eventsMu.Lock()
						defer eventsMu.Unlock()
						events = append(events, e)
						return nil
					}}
					if policy == UnknownMethodCustom {
						opt.UnknownMethod.Handler = UnknownMethodHandlerFunc(func(ctx context.Context, r UnknownMethodRequest) (UnknownMethodDecision, error) {
							factsMu.Lock()
							defer factsMu.Unlock()
							facts = append(facts, r)
							id, _ := IngressRequestIDFromContext(ctx)
							if id != r.RequestID {
								panic("identity")
							}
							return UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: UnknownMethodRejection{Status: 429, Code: "RATE_LIMIT", Message: "slow down"}}, nil
						})
					}
					ingressActivate(t, f, opt)
					original := f.runtime()
					if err := f.initialize(); err != nil || f.runtime() != original {
						t.Fatal("initialization not idempotent", err)
					}
					request := ingressTransport(t, f, transport)
					unknown := []string{"X-" + uuid.NewString(), "get", "gEt", "!#$%&'*+-.^_`|~"}
					for _, m := range unknown {
						for _, path := range []string{"/healthz", "/missing", "/nested//path/"} {
							r, b := request(m, path)
							if policy != UnknownMethodNative {
								status, code := 501, "HTTP_METHOD_NOT_IMPLEMENTED"
								if policy == UnknownMethodCustom {
									status, code = 429, "RATE_LIMIT"
								}
								ingressEnvelope(t, r, b, status, code)
							} else if r.Header.Get(XRequestID) == "" {
								t.Fatal("early native exit lost ID")
							}
						}
					}
					for _, m := range []string{"GET", "HEAD", "OPTIONS", "POST", "X-KNOWN"} {
						r, b := request(m, "/healthz")
						if m == "HEAD" && len(b) != 0 {
							t.Fatal("HEAD body")
						}
						if r.Header.Get(XRequestID) == "" {
							t.Fatal("missing ID")
						}
					}
					eventsMu.Lock()
					defer eventsMu.Unlock()
					ids := map[string]bool{}
					for _, e := range events {
						if ids[e.RequestID] {
							t.Fatal("reused ID")
						}
						ids[e.RequestID] = true
						want := uint8(1)
						if e.Outcome == IngressRejected {
							want = 0
						}
						if e.Dispatches != want {
							t.Fatal(e)
						}
					}
					if len(events) != 17 || calls.Load() != 4 {
						t.Fatalf("events=%d handlers=%d", len(events), calls.Load())
					}
					factsMu.Lock()
					defer factsMu.Unlock()
					if policy == UnknownMethodCustom && len(facts) != 12 {
						t.Fatalf("calls=%d", len(facts))
					}
					for _, f := range facts {
						if f.Method == "" || f.RequestID == "" {
							t.Fatal("pooled facts")
						}
					}
				})
			}
		}
	}
}

func TestBoundedIngressNativeEquivalence(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, transport := range []string{"test", "socket"} {
			for _, shape := range []string{"empty", "middleware", "mounted"} {
				t.Run(major+"/"+transport+"/"+shape, func(t *testing.T) {
					control, enabled := newIngressFixture(major, false), newIngressFixture(major, false)
					for _, f := range []ingressFixture{control, enabled} {
						if shape == "middleware" {
							f.use(func(next HandlerFunc) HandlerFunc {
								return func(c Context) error { c.SetHeader("X-Native", "kept"); return next(c) }
							})
						}
						if shape == "mounted" {
							f.register("GET", "/mount/thing/:id", func(c Context) error { c.SetHeader("X-Native", "kept"); return c.SendString("body") })
							f.register("OPTIONS", "/mount/thing/:id", func(c Context) error { return c.NoContent(204) })
						}
					}
					if err := control.initialize(); err != nil {
						t.Fatal(err)
					}
					ingressActivate(t, enabled, BoundedIngressOptions{})
					left, right := ingressTransport(t, control, transport), ingressTransport(t, enabled, transport)
					for _, m := range []string{"X-" + uuid.NewString(), "get", "GET", "HEAD", "OPTIONS", "POST"} {
						for _, p := range []string{"/mount/thing/1", "/mount//thing/1/", "/missing"} {
							a, ab := left(m, p)
							b, bb := right(m, p)
							for _, h := range []string{XRequestID, "Date", "Connection", "Content-Length"} {
								a.Header.Del(h)
								b.Header.Del(h)
							}
							if a.StatusCode != b.StatusCode || string(ab) != string(bb) || !reflect.DeepEqual(a.Header, b.Header) {
								t.Fatalf("%s %s\n%d %s %#v\n%d %s %#v", m, p, a.StatusCode, ab, a.Header, b.StatusCode, bb, b.Header)
							}
						}
					}
				})
			}
		}
	}
}

func TestBoundedIngressConfigurationLifecycle(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, scenario := range []string{"retry", "sticky", "repeated", "late", "start-failure", "methods-before", "methods-after", "routes-before", "routes-after", "handler"} {
			t.Run(major+"/"+scenario, func(t *testing.T) {
				f := newIngressFixture(major, false)
				good := BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodReject}}
				bad := BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodPolicy(255)}}
				switch scenario {
				case "retry", "sticky":
					first := f.configure(bad)
					if first == nil {
						t.Fatal("accepted invalid")
					}
					if scenario == "sticky" {
						if f.initialize() != first || f.initialize() != first {
							t.Fatal("not sticky")
						}
						if f.runtime() != nil {
							t.Fatal("installed failed policy")
						}
						if f.configure(good) == nil {
							t.Fatal("late retry")
						}
						return
					}
					if err := f.configure(good); err != nil {
						t.Fatal(err)
					}
				case "late":
					if err := f.initialize(); err != nil {
						t.Fatal(err)
					}
					if err := f.configure(good); err == nil {
						t.Fatal("late accepted")
					}
					return
				case "start-failure":
					_, err := f.managed.StartManaged(nil, managedOptions())
					if err == nil {
						t.Fatal("nil parent accepted")
					}
					var configErr *IngressConfigurationError
					if !errors.As(f.configure(good), &configErr) || configErr.Rule != "closed" {
						t.Fatal("failed start did not close config")
					}
					return
				default:
					if err := f.configure(good); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "repeated" {
					var e *IngressConfigurationError
					if !errors.As(f.configure(bad), &e) || e.Rule != "already_configured" {
						t.Fatal("replacement changed selection")
					}
				}
				if scenario == "methods-before" {
					f.methods[0] = "CHANGED"
				}
				if scenario == "routes-before" {
					f.mutateRoute()
				}
				if scenario == "handler" {
					f.server.Handler = fasthttp.TimeoutHandler(f.server.Handler, time.Second, "secret")
				}
				// Successful configuration has not sealed routes or installed the boundary.
				if f.runtime() != nil {
					t.Fatal("configured means installed")
				}
				f.register("GET", "/late", func(c Context) error { return c.SendString("late") })
				err := f.initialize()
				if scenario == "methods-before" || scenario == "routes-before" || scenario == "handler" {
					if err == nil {
						t.Fatal("unsupported mutation accepted")
					}
					if f.initialize() != err {
						t.Fatal("initialization failure not sticky")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "methods-after" {
					f.methods[0] = "CHANGED"
				}
				if scenario == "routes-after" {
					f.mutateRoute()
				}
				r, b := ingressTransport(t, f, "test")("X-UNKNOWN", "/late")
				if scenario == "methods-after" || scenario == "routes-after" {
					ingressEnvelope(t, r, b, 500, "INGRESS_POLICY_FAILED")
				} else {
					ingressEnvelope(t, r, b, 501, "HTTP_METHOD_NOT_IMPLEMENTED")
				}
			})
		}
	}
}

func TestBoundedIngressRuntimeFailures(t *testing.T) {
	secret := "SECRET-causes-must-stay-privileged"
	for _, major := range []string{"v2", "v3"} {
		for _, mode := range []string{"error", "panic", "zero", "action", "mixed", "observer-error", "observer-panic", "dispatch-panic", "serialization"} {
			t.Run(major+"/"+mode, func(t *testing.T) {
				f := newIngressFixture(major, false)
				var observed atomic.Int32
				var diagnostics atomic.Int32
				opt := BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
					switch mode {
					case "error":
						return UnknownMethodDecision{}, errors.New(secret)
					case "panic":
						panic(secret)
					case "zero":
						return UnknownMethodDecision{}, nil
					case "action":
						return UnknownMethodDecision{Action: 99}, nil
					case "mixed":
						return UnknownMethodDecision{Action: DelegateNative, Rejection: ingressConnect}, nil
					}
					return UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: ingressConnect}, nil
				})}, Observer: func(_ IngressCancellation, e IngressEvent) error {
					observed.Add(1)
					if mode == "observer-error" {
						return errors.New(secret)
					}
					if mode == "observer-panic" {
						panic(secret)
					}
					return nil
				}, Diagnostic: func(_ IngressCancellation, d IngressDiagnostic) { diagnostics.Add(1); panic("diagnostic-secret") }}
				f.register("GET", "/panic", func(Context) error { panic(secret) })
				ingressActivate(t, f, opt)
				if mode == "serialization" { // Internal fault injection; no application serializer seam.
					f.runtime().options.UnknownMethod = UnknownMethodOptions{Policy: UnknownMethodReject, Rejection: UnknownMethodRejection{Status: 501, Code: "CODE", Message: strings.Repeat("x", IngressMaxEnvelopeBytes)}}
				}
				method, path := "X-UNKNOWN", "/missing"
				if mode == "dispatch-panic" {
					method, path = "GET", "/panic"
				}
				r, b := ingressTransport(t, f, "test")(method, path)
				status, code := 500, "INGRESS_POLICY_FAILED"
				if strings.HasPrefix(mode, "observer") {
					status, code = 501, "HTTP_METHOD_NOT_IMPLEMENTED"
				}
				ingressEnvelope(t, r, b, status, code)
				if strings.Contains(string(b), secret) || observed.Load() != 1 || diagnostics.Load() != 1 {
					t.Fatalf("leak/reentry: %s events=%d diagnostics=%d", b, observed.Load(), diagnostics.Load())
				}
			})
		}
	}
}

func TestBoundedIngressRuntimeStatuses(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, transport := range []string{"test", "socket"} {
			t.Run(major+"/"+transport, func(t *testing.T) {
				statuses := []int{-1, 0, 399, 600}
				for status := 400; status <= 599; status++ {
					statuses = append(statuses, status)
				}
				for _, status := range statuses {
					t.Run(fmt.Sprint(status), func(t *testing.T) {
						rejection := UnknownMethodRejection{Status: status, Code: "PUBLIC_CODE", Message: "public message"}
						valid := status >= 400 && status <= 599 && status != 401 && status != 405 && status != 407 && status != 426
						static := newIngressFixture(major, false)
						err := static.configure(BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodReject, Rejection: rejection}})
						if (err == nil) != valid {
							t.Fatal("static status", err)
						}
						if !valid && static.initialize() == nil {
							t.Fatal("invalid setup started")
						}
						dynamic := newIngressFixture(major, false)
						var events atomic.Int32
						ingressActivate(t, dynamic, BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
							return UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: rejection}, nil
						})}, Observer: func(_ IngressCancellation, e IngressEvent) error {
							events.Add(1)
							if e.Dispatches != 0 {
								t.Error("invalid decision dispatched")
							}
							return nil
						}})
						r, b := ingressTransport(t, dynamic, transport)("X-UNKNOWN", "/")
						want, code := status, "PUBLIC_CODE"
						if !valid {
							want, code = 500, "INGRESS_POLICY_FAILED"
						}
						ingressEnvelope(t, r, b, want, code)
						if events.Load() != 1 {
							t.Fatal(events.Load())
						}
						if valid {
							if err := static.initialize(); err != nil {
								t.Fatal(err)
							}
							r, b = ingressTransport(t, static, transport)("X-UNKNOWN", "/")
							ingressEnvelope(t, r, b, status, "PUBLIC_CODE")
						}
					})
				}
			})
		}
	}
}

func TestBoundedIngressWireTargets(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			var strategy atomic.Int32
			var events atomic.Int32
			ingressActivate(t, f, BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
				strategy.Add(1)
				return UnknownMethodDecision{Action: DelegateNative}, nil
			})}, Observer: func(IngressCancellation, IngressEvent) error { events.Add(1); return nil }})
			run := managedStart(t, managedFixture{server: f.managed}, context.Background(), managedOptions())
			managedReady(t, run)
			for _, target := range []string{"example.test:443", "127.0.0.1:80", "[::1]:443", "/path", "example.test", "host:0", "host:65536", "host:nope", "[example.test]:443", "[127.0.0.1]:443", "bad!host:443"} {
				t.Run(target, func(t *testing.T) {
					r, b := ingressWire(t, run.Address(), "CONNECT", target)
					status := 501
					if target != "example.test:443" && target != "127.0.0.1:80" && target != "[::1]:443" {
						status = 400
					}
					if r.StatusCode != status {
						t.Fatalf("%d %s", r.StatusCode, b)
					}
					if r.Header.Get(XRequestID) != "" {
						code := "HTTP_METHOD_NOT_IMPLEMENTED"
						if status == 400 {
							code = "HTTP_REQUEST_INVALID"
						}
						ingressEnvelope(t, r, b, status, code)
					} else if status == 501 {
						t.Fatal("valid CONNECT bypassed ingress")
					}
				})
			}
			if strategy.Load() != 0 {
				t.Fatal("CONNECT invoked strategy")
			}
			r, _ := ingressWire(t, run.Address(), "OPTIONS", "*")
			if r.Header.Get(XRequestID) == "" {
				t.Fatal("OPTIONS* lost boundary")
			}
			if strategy.Load() != 0 {
				t.Fatal("OPTIONS* strategy")
			}
			r, _ = ingressWire(t, run.Address(), "X-UNKNOWN", "/missing")
			if r.Header.Get(XRequestID) == "" || strategy.Load() != 1 {
				t.Fatal("wire unknown escaped boundary")
			}
			r, _ = ingressWire(t, run.Address(), "B(AD", "/")
			if r.StatusCode < 400 || r.StatusCode > 599 || r.Header.Get(XRequestID) != "" && r.StatusCode != 400 {
				t.Fatal("malformed method accepted", r.StatusCode)
			}
		})
	}
}
func ingressWire(t *testing.T, address, method, target string) (*http.Response, []byte) {
	t.Helper()
	c, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(c, "%s %s HTTP/1.1\r\nHost: example.test\r\nX-Request-ID: untrusted-client-id\r\nConnection: close\r\n\r\n", method, target)
	r, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return r, b
}

func TestBoundedIngressManagedCallbacks(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, phase := range []string{"strategy", "observer", "diagnostic"} {
			for _, cooperative := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/cooperative=%t", major, phase, cooperative), func(t *testing.T) {
					f := newIngressFixture(major, false)
					entered := make(chan struct{})
					canceled := make(chan struct{})
					release := make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					defer unblock()
					wait := func(signal IngressCancellation) {
						if signal.Err() != nil {
							t.Error("premature cancellation")
						}
						close(entered)
						<-signal.Done()
						if signal.Err() != context.Canceled {
							t.Error("unexpected error", signal.Err())
						}
						close(canceled)
						if !cooperative {
							<-release
						}
					}
					options := BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(ctx context.Context, _ UnknownMethodRequest) (UnknownMethodDecision, error) {
						if phase == "strategy" {
							wait(IngressCancellationFromContext(ctx))
						}
						if phase == "diagnostic" {
							return UnknownMethodDecision{}, errors.New("private")
						}
						return UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: ingressConnect}, nil
					})}}
					if phase == "observer" {
						options.Observer = func(signal IngressCancellation, _ IngressEvent) error { wait(signal); return nil }
					}
					if phase == "diagnostic" {
						options.Diagnostic = func(signal IngressCancellation, _ IngressDiagnostic) { wait(signal) }
					}
					ingressActivate(t, f, options)
					parent, cancel := context.WithCancel(context.Background())
					defer cancel()
					opts := managedOptions()
					opts.DrainTimeout = 80 * time.Millisecond
					run := managedStart(t, managedFixture{server: f.managed}, parent, opts)
					managedReady(t, run)
					requestDone := make(chan struct{})
					go func() {
						defer close(requestDone)
						req, _ := http.NewRequest("X-UNKNOWN", "http://"+run.Address()+"/", nil)
						client := http.Client{Timeout: 2 * time.Second}
						resp, _ := client.Do(req)
						if resp != nil {
							resp.Body.Close()
						}
					}()
					managedAwait(t, entered)
					cancel()
					managedAwait(t, run.Stopping())
					// Parent cancellation starts drain; it must not cancel admitted work early.
					select {
					case <-canceled:
						t.Fatal("canceled before drain budget")
					case <-time.After(10 * time.Millisecond):
					}
					managedAwait(t, canceled)
					if !cooperative {
						select {
						case <-run.Done():
							t.Fatal("abandoned callback")
						default:
						}
						ctx, c := context.WithTimeout(context.Background(), time.Second)
						err := run.Stop(ctx)
						c()
						if !errors.Is(err, ErrManagedDrainTimeout) {
							t.Fatal("bounded stop", err)
						}
						unblock()
					}
					managedAwait(t, run.Done())
					managedAwait(t, requestDone)
				})
			}
		}
	}
}

func TestBoundedIngressAdmission(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			var strategies atomic.Int32
			var native atomic.Int32
			events := make(chan IngressEvent, 4)
			ingressActivate(t, f, BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
				strategies.Add(1)
				return UnknownMethodDecision{Action: DelegateNative}, nil
			})}, Observer: func(signal IngressCancellation, e IngressEvent) error {
				if e.Outcome == IngressAdmissionDenied {
					if signal.Err() != context.Canceled {
						t.Error("denial signal live")
					}
					select {
					case <-signal.Done():
					default:
						t.Error("denial not canceled")
					}
				}
				events <- e
				return nil
			}})
			run := managedStart(t, managedFixture{server: f.managed}, context.Background(), managedOptions())
			managedReady(t, run)
			// Exercise the installed composed boundary after the real stop transition.
			if err := run.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			managedAwait(t, run.Done())
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetMethod("X-UNKNOWN")
			ctx.Request.SetRequestURI("/")
			f.runtime().native = func(*fasthttp.RequestCtx) { native.Add(1) }
			f.server.Handler(ctx)
			if ctx.Response.StatusCode() != 503 || strategies.Load() != 0 || native.Load() != 0 {
				t.Fatal("denied work dispatched")
			}
			e := <-events
			if e.Outcome != IngressAdmissionDenied || e.Dispatches != 0 || e.RequestID != string(ctx.Response.Header.Peek(XRequestID)) {
				t.Fatal(e)
			}
		})
	}
}

func TestBoundedIngressConcurrentContextReplacement(t *testing.T) {
	type trustedKey struct{}
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			seen := sync.Map{}
			var count atomic.Int32
			f.use(func(next HandlerFunc) HandlerFunc {
				return func(c Context) error {
					before, _ := IngressRequestIDFromContext(c.Context())
					c.SetContext(context.WithValue(context.Background(), trustedKey{}, "trusted"))
					after, _ := IngressRequestIDFromContext(c.Context())
					if before != after || after == "" || c.Context().Value(trustedKey{}) != "trusted" {
						return errors.New("replacement lost ownership")
					}
					return next(c)
				}
			})
			f.register("GET", "/context", func(c Context) error {
				id, _ := IngressRequestIDFromContext(c.Context())
				requestID, _ := IngressRequestIDFromContext(c.(interface{ Request() *http.Request }).Request().Context())
				if requestID != id || id != c.Header(XRequestID) {
					return errors.New("inconsistent request")
				}
				if _, loaded := seen.LoadOrStore(id, true); loaded {
					return errors.New("shared ID")
				}
				count.Add(1)
				c.SetHeader(XRequestID, "downstream-replacement")
				return c.SendString(id)
			})
			ingressActivate(t, f, BoundedIngressOptions{})
			run := managedStart(t, managedFixture{server: f.managed}, context.Background(), managedOptions())
			managedReady(t, run)
			var wg sync.WaitGroup
			tr := &http.Transport{}
			defer tr.CloseIdleConnections()
			client := http.Client{Transport: tr, Timeout: 3 * time.Second}
			for n := 0; n < 40; n++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					req, _ := http.NewRequest("GET", "http://"+run.Address()+"/context", nil)
					req.Header.Set(XRequestID, "client-secret")
					res, err := client.Do(req)
					if err != nil {
						t.Error(err)
						return
					}
					b, err := io.ReadAll(res.Body)
					res.Body.Close()
					if err != nil || res.StatusCode != 200 || res.Header.Get(XRequestID) != string(b) || string(b) == "client-secret" {
						t.Error("lost identity", res.StatusCode, string(b), err)
					}
				}()
			}
			wg.Wait()
			if count.Load() != 40 {
				t.Fatal(count.Load())
			}
		})
	}
}

type ingressStreamReader struct {
	ctx      context.Context
	read     bool
	canceled chan struct{}
}

func (r *ingressStreamReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		select {
		case <-r.ctx.Done():
			return 0, errors.New("stream canceled before read")
		default:
		}
		return copy(p, "stream"), nil
	}
	<-r.ctx.Done()
	close(r.canceled)
	return 0, io.EOF
}
func TestBoundedIngressCommitment(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, kind := range []string{"body", "writer", "stream"} {
			t.Run(major+"/"+kind, func(t *testing.T) {
				f := newIngressFixture(major, false)
				events := make(chan IngressEvent, 1)
				streamCanceled := make(chan struct{})
				var diagnostics atomic.Int32
				f.register("GET", "/commit", func(c Context) error {
					switch kind {
					case "body":
						_ = c.SendString("body")
					case "writer":
						_, _ = c.(interface{ Response() http.ResponseWriter }).Response().Write([]byte("writer"))
					case "stream":
						c.SetContext(context.WithValue(context.Background(), struct{}{}, "replacement"))
						_ = c.SendStream(&ingressStreamReader{ctx: c.Context(), canceled: streamCanceled})
					}
					panic("private-after-commit")
				})
				ingressActivate(t, f, BoundedIngressOptions{Observer: func(_ IngressCancellation, e IngressEvent) error { events <- e; return nil }, Diagnostic: func(IngressCancellation, IngressDiagnostic) { diagnostics.Add(1) }})
				if kind != "stream" {
					r, b := ingressTransport(t, f, "test")("GET", "/commit")
					if r.StatusCode != 200 || string(b) != kind {
						t.Fatalf("rewrote committed response %d %s", r.StatusCode, b)
					}
				} else {
					opts := managedOptions()
					opts.DrainTimeout = 50 * time.Millisecond
					run := managedStart(t, managedFixture{server: f.managed}, context.Background(), opts)
					managedReady(t, run)
					conn, err := net.DialTimeout("tcp", run.Address(), time.Second)
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
					fmt.Fprint(conn, "GET /commit HTTP/1.1\r\nHost: test\r\n\r\n")
					e := <-events
					if !e.Committed || e.Status != 200 || e.Dispatches != 1 {
						t.Fatal(e)
					}
					_ = run.Stop(context.Background())
					managedAwait(t, streamCanceled)
					managedAwait(t, run.Done())
					if diagnostics.Load() != 1 {
						t.Fatal("panic not contained")
					}
					return
				}
				e := <-events
				if !e.Committed || e.Status != 200 || e.Dispatches != 1 || diagnostics.Load() != 1 {
					t.Fatal(e, diagnostics.Load())
				}
			})
		}
	}
}

type unsupportedIngressCtx struct{ f3.DefaultCtx }

func TestBoundedIngressCustomContextRejected(t *testing.T) {
	var acquired atomic.Int32
	app := f3.NewWithCustomCtx(func(app *f3.App) f3.CustomCtx {
		acquired.Add(1)
		return &unsupportedIngressCtx{DefaultCtx: *f3.NewDefaultCtx(app)}
	})
	a := NewFiberV3Adapter(func(*f3.App) *f3.App { return app }).(*FiberV3Adapter)
	if err := a.ConfigureBoundedIngress(BoundedIngressOptions{}); err != nil {
		t.Fatal(err)
	}
	before := acquired.Load()
	err := a.Initialize()
	var config *IngressConfigurationError
	if !errors.As(err, &config) || config.Field != "native.context" || config.Rule != "unsupported" {
		t.Fatal(err)
	}
	if a.ingress.runtime != nil || a.Initialize() != err || acquired.Load() != before+1 {
		t.Fatal("unsupported context installed/retried")
	}
}

func TestBoundedIngressCanceledCallbackEntry(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			entered := make(chan struct{})
			var calls atomic.Int32
			check := func(c IngressCancellation) {
				calls.Add(1)
				if c.Err() != context.Canceled {
					t.Error("callback entered with live lifetime", c.Err())
				}
				select {
				case <-c.Done():
				default:
					t.Error("callback has open Done")
				}
			}
			ingressActivate(t, f, BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(ctx context.Context, _ UnknownMethodRequest) (UnknownMethodDecision, error) {
				close(entered)
				<-ctx.Done()
				return UnknownMethodDecision{}, errors.New("private")
			})}, Observer: func(c IngressCancellation, _ IngressEvent) error { check(c); return nil }, Diagnostic: func(c IngressCancellation, _ IngressDiagnostic) { check(c) }})
			opts := managedOptions()
			opts.DrainTimeout = 30 * time.Millisecond
			run := managedStart(t, managedFixture{server: f.managed}, context.Background(), opts)
			managedReady(t, run)
			done := make(chan struct{})
			go func() {
				defer close(done)
				req, _ := http.NewRequest("X-UNKNOWN", "http://"+run.Address()+"/", nil)
				client := http.Client{Timeout: time.Second}
				res, _ := client.Do(req)
				if res != nil {
					res.Body.Close()
				}
			}()
			managedAwait(t, entered)
			_ = run.Stop(context.Background())
			managedAwait(t, run.Done())
			managedAwait(t, done)
			if calls.Load() != 2 {
				t.Fatal(calls.Load())
			}
		})
	}
}

func TestBoundedIngressKeepaliveDrain(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var strategies atomic.Int32
			var native atomic.Int32
			f.register("GET", "/held", func(c Context) error { native.Add(1); close(entered); <-release; return c.SendString("first") })
			ingressActivate(t, f, BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
				strategies.Add(1)
				return UnknownMethodDecision{Action: DelegateNative}, nil
			})}})
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			run := managedStart(t, managedFixture{server: f.managed}, parent, managedOptions())
			managedReady(t, run)
			conn, err := net.DialTimeout("tcp", run.Address(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			fmt.Fprint(conn, "GET /held HTTP/1.1\r\nHost: test\r\n\r\nX-UNKNOWN /held HTTP/1.1\r\nHost: test\r\n\r\n")
			managedAwait(t, entered)
			cancel()
			managedAwait(t, run.Stopping())
			once.Do(func() { close(release) })
			reader := bufio.NewReader(conn)
			res, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if string(body) != "first" {
				t.Fatal(string(body))
			}
			second, err := http.ReadResponse(reader, &http.Request{Method: "X-UNKNOWN"})
			if err == nil {
				second.Body.Close()
				if second.StatusCode != 503 {
					t.Fatal("new work admitted", second.StatusCode)
				}
			}
			managedAwait(t, run.Done())
			if strategies.Load() != 0 || native.Load() != 1 {
				t.Fatal("drain dispatch", strategies.Load(), native.Load())
			}
		})
	}
}

func TestBoundedIngressHijackOwnership(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			events := make(chan IngressEvent, 1)
			ended := make(chan struct{})
			var socket WebSocketContext
			var socketMu sync.Mutex
			handler := func(c WebSocketContext) error {
				defer close(ended)
				socketMu.Lock()
				socket = c
				socketMu.Unlock()
				id, _ := IngressRequestIDFromContext(c.Context())
				if id == "" {
					t.Error("hijack lost identity")
				}
				if err := c.WriteMessage(1, []byte(id)); err != nil {
					return err
				}
				_, _, err := c.ReadMessage()
				return err
			}
			cfg := DefaultWebSocketConfig()
			cfg.DisableReadDeadline = true
			switch a := f.managed.(type) {
			case *FiberAdapter:
				a.Router().WebSocket("/ws", cfg, handler)
			case *FiberV3Adapter:
				a.Router().WebSocket("/ws", cfg, handler)
			}
			ingressActivate(t, f, BoundedIngressOptions{Observer: func(_ IngressCancellation, e IngressEvent) error { events <- e; return nil }})
			opts := managedOptions()
			opts.OnShutdown = func(ctx context.Context) error {
				socketMu.Lock()
				s := socket
				socketMu.Unlock()
				if s != nil {
					_ = s.Close()
				}
				select {
				case <-ended:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			run := managedStart(t, managedFixture{server: f.managed}, context.Background(), opts)
			managedReady(t, run)
			ws, res, err := (&websocket.Dialer{HandshakeTimeout: time.Second}).Dial("ws://"+run.Address()+"/ws", http.Header{XRequestID: []string{"client"}})
			if res != nil && res.Body != nil {
				res.Body.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			_ = ws.SetReadDeadline(time.Now().Add(time.Second))
			_, id, err := ws.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if res.Header.Get(XRequestID) != string(id) || string(id) == "client" {
				t.Fatal("hijack correlation", res.Header, string(id))
			}
			select {
			case e := <-events:
				if !e.Committed || e.Dispatches != 1 {
					t.Fatal(e)
				}
			case <-time.After(time.Second):
				t.Fatal("missing observation")
			}
			if err := run.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			managedAwait(t, run.Done())
			managedAwait(t, ended)
		})
	}
}

type ingressFiniteStream struct {
	ctx     context.Context
	done    bool
	checked chan struct{}
}

func (s *ingressFiniteStream) Read(p []byte) (int, error) {
	if s.done {
		return 0, io.EOF
	}
	s.done = true
	if s.ctx.Err() != nil {
		return 0, errors.New("canceled before stream write")
	}
	close(s.checked)
	return copy(p, "stream-live"), nil
}
func TestBoundedIngressStandaloneStream(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			captured := make(chan context.Context, 1)
			checked := make(chan struct{})
			f.register("GET", "/stream", func(c Context) error {
				ctx := c.Context()
				captured <- ctx
				return c.SendStream(&ingressFiniteStream{ctx: ctx, checked: checked})
			})
			ingressActivate(t, f, BoundedIngressOptions{})
			res, b := ingressTransport(t, f, "test")("GET", "/stream")
			if res.StatusCode != 200 || string(b) != "stream-live" {
				t.Fatal(res.StatusCode, string(b))
			}
			managedAwait(t, checked)
			ctx := <-captured
			managedAwait(t, ctx.Done())
		})
	}
}

func TestBoundedIngressOptionCopiesAndRewrite(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			retry := true
			opts := BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodReject, Rejection: UnknownMethodRejection{Status: 429, Code: "WAIT", Message: "public", Retryable: &retry}}}
			ingressActivate(t, f, opts)
			retry = false
			opts.UnknownMethod.Rejection.Code = "MUTATED"
			r, b := ingressTransport(t, f, "test")("X-UNKNOWN", "/")
			ingressEnvelope(t, r, b, 429, "WAIT")
			var e IngressErrorEnvelope
			_ = json.Unmarshal(b, &e)
			if e.Retryable == nil || !*e.Retryable {
				t.Fatal("shared config")
			}
			rewrite := newIngressFixture(major, false)
			var strategy atomic.Int32
			rewrite.register("GET", "/rewrite", func(c Context) error {
				switch c := c.(type) {
				case *fiberContext:
					c.ctx.Method("X-REWRITTEN")
				case *fiberV3Context:
					c.ctx.Method("X-REWRITTEN")
				}
				return c.SendString("rewritten")
			})
			ingressActivate(t, rewrite, BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
				strategy.Add(1)
				return UnknownMethodDecision{Action: DelegateNative}, nil
			})}})
			r, b = ingressTransport(t, rewrite, "test")("GET", "/rewrite")
			if r.StatusCode != 200 || string(b) != "rewritten" || strategy.Load() != 0 {
				t.Fatal("classified after rewrite")
			}
		})
	}
}

func TestBoundedIngressNativeMiddlewareContext(t *testing.T) {
	type key struct{}
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			if major == "v2" {
				f.use(MiddlewareFromFiber(func(c *f2.Ctx) error {
					c.SetUserContext(context.WithValue(context.Background(), key{}, "native"))
					return c.Next()
				}))
			} else {
				f.use(MiddlewareFromFiberV3(func(c f3.Ctx) error {
					c.SetContext(context.WithValue(context.Background(), key{}, "native"))
					return c.Next()
				}))
			}
			f.register("GET", "/", func(c Context) error {
				id, _ := IngressRequestIDFromContext(c.Context())
				if id == "" || c.Context().Value(key{}) != "native" {
					return c.SendStatus(500)
				}
				return c.SendString(id)
			})
			ingressActivate(t, f, BoundedIngressOptions{})
			r, b := ingressTransport(t, f, "test")("GET", "/")
			if r.StatusCode != 200 || string(b) != r.Header.Get(XRequestID) {
				t.Fatal(r.StatusCode, string(b), r.Header)
			}
		})
	}
}

func TestBoundedIngressNativeListener(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			var start func(net.Listener) error
			var stop func(context.Context) error
			if major == "v2" {
				a := NewFiberAdapter(func(*f2.App) *f2.App { return f2.New(f2.Config{DisableStartupMessage: true}) }).(*FiberAdapter)
				if err := a.ConfigureBoundedIngress(BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodReject}}); err != nil {
					t.Fatal(err)
				}
				app := a.WrappedRouter()
				start = app.Listener
				stop = app.ShutdownWithContext
			} else {
				a := NewFiberV3Adapter().(*FiberV3Adapter)
				if err := a.ConfigureBoundedIngress(BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodReject}}); err != nil {
					t.Fatal(err)
				}
				app := a.WrappedRouter()
				start = func(ln net.Listener) error { return app.Listener(ln, f3.ListenConfig{DisableStartupMessage: true}) }
				stop = app.ShutdownWithContext
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- start(ln) }()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = stop(ctx)
				_ = ln.Close()
				select {
				case err := <-done:
					if err != nil && !errors.Is(err, net.ErrClosed) {
						t.Error(err)
					}
				case <-ctx.Done():
					t.Error("native listener did not join")
				}
			})
			res, body := ingressWire(t, ln.Addr().String(), "X-NATIVE-LISTENER", "/missing")
			ingressEnvelope(t, res, body, 501, "HTTP_METHOD_NOT_IMPLEMENTED")
		})
	}
}
func TestBoundedIngressClassificationAvoidsContextFactory(t *testing.T) {
	var calls atomic.Int32
	app := f3.NewWithCustomCtx(func(app *f3.App) f3.CustomCtx { calls.Add(1); return f3.NewDefaultCtx(app) })
	a := NewFiberV3Adapter(func(*f3.App) *f3.App { return app }).(*FiberV3Adapter)
	if err := a.ConfigureBoundedIngress(BoundedIngressOptions{UnknownMethod: UnknownMethodOptions{Policy: UnknownMethodReject}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Initialize(); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest("X-"+uuid.NewString(), "http://example.test/", nil)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 501 {
			t.Fatal(res.StatusCode)
		}
	}
	if calls.Load() != before {
		t.Fatal("classification acquired native context")
	}
}

func TestBoundedIngressValidationPrecedence(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := newIngressFixture(major, false)
			var observed atomic.Int32
			ingressActivate(t, f, BoundedIngressOptions{Observer: func(c IngressCancellation, e IngressEvent) error {
				observed.Add(1)
				if c.Err() != context.Canceled || e.Outcome != IngressInvalidRequest || e.Status != 400 || e.Dispatches != 0 {
					t.Error("validation precedence", c.Err(), e)
				}
				return nil
			}})
			for _, admitted := range []bool{true, false} {
				c := &fasthttp.RequestCtx{}
				c.Request.Header.SetMethod("CONNECT")
				c.Request.SetRequestURI("/invalid")
				f.runtime().handle(c, context.Background(), admitted)
				if c.Response.StatusCode() != 400 {
					t.Fatal(c.Response.StatusCode())
				}
			}
			if observed.Load() != 2 {
				t.Fatal(observed.Load())
			}
		})
	}
}
