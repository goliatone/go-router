package router_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	fiber2 "github.com/gofiber/fiber/v2"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

type consumerUnknownMethodHandler struct{}

func (consumerUnknownMethodHandler) HandleUnknownMethod(context.Context, router.UnknownMethodRequest) (router.UnknownMethodDecision, error) {
	return router.UnknownMethodDecision{Action: router.RejectUnknownMethod, Rejection: router.UnknownMethodRejection{Status: 429, Code: "SLOW_DOWN", Message: "try later"}}, nil
}

var _ router.UnknownMethodHandler = consumerUnknownMethodHandler{}
var _ router.UnknownMethodHandler = router.UnknownMethodHandlerFunc(nil)

// Activation is an optional capability contract. This consumer compiles without
// adding a method to Server. Adapter activation/conformance is T17, not preflight.
func compileBoundedIngressConsumer(s interface {
	router.BoundedIngressServer
	router.StartupInitializer
}) error {
	if err := s.ConfigureBoundedIngress(router.BoundedIngressOptions{UnknownMethod: router.UnknownMethodOptions{Policy: router.UnknownMethodCustom, Handler: consumerUnknownMethodHandler{}}}); err != nil {
		return err
	}
	// Configuration only stores policy; Initialize establishes installation.
	return s.Initialize()
}

func TestBoundedIngressAdapterPreflight(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			var validator router.BoundedIngressValidator
			var request func(*http.Request) (*http.Response, error)
			var register func()
			var methods []string
			calls := 0
			switch major {
			case "v2":
				methods = append(append([]string(nil), fiber2.DefaultMethods...), "X-CUSTOM")
				app := fiber2.New(fiber2.Config{RequestMethods: methods, DisableStartupMessage: true})
				server := router.NewFiberAdapter(func(*fiber2.App) *fiber2.App { return app })
				validator = server.(router.BoundedIngressValidator)
				request = func(r *http.Request) (*http.Response, error) { return server.WrappedRouter().Test(r) }
				register = func() { server.Router().Get("/legacy", func(c router.Context) error { return c.SendString("legacy") }) }
			case "v3":
				methods = append(append([]string(nil), fiber3.DefaultMethods...), "X-CUSTOM")
				app := fiber3.New(fiber3.Config{RequestMethods: methods})
				server := router.NewFiberV3Adapter(func(*fiber3.App) *fiber3.App { return app })
				validator = server.(router.BoundedIngressValidator)
				request = func(r *http.Request) (*http.Response, error) { return server.WrappedRouter().Test(r) }
				register = func() { server.Router().Get("/legacy", func(c router.Context) error { return c.SendString("legacy") }) }
			}
			handler := router.UnknownMethodHandlerFunc(func(context.Context, router.UnknownMethodRequest) (router.UnknownMethodDecision, error) {
				calls++
				return router.UnknownMethodDecision{Action: router.DelegateNative}, nil
			})
			for _, unknown := range []router.UnknownMethodOptions{{}, {Policy: router.UnknownMethodReject}, {Policy: router.UnknownMethodCustom, Handler: handler}, {Policy: router.UnknownMethodCustom, Handler: consumerUnknownMethodHandler{}}} {
				if err := validator.ValidateBoundedIngress(router.BoundedIngressOptions{UnknownMethod: unknown, Observer: func(router.IngressCancellation, router.IngressEvent) error { calls++; return nil }, Diagnostic: func(router.IngressCancellation, router.IngressDiagnostic) { calls++ }}); err != nil {
					t.Fatal(err)
				}
			}
			// Every permitted/forbidden status is checked on both native adapters before
			// any request or route finalization. In particular, 426 cannot reach serving.
			for status := 399; status <= 600; status++ {
				err := validator.ValidateBoundedIngress(router.BoundedIngressOptions{UnknownMethod: router.UnknownMethodOptions{Policy: router.UnknownMethodReject, Rejection: router.UnknownMethodRejection{Status: status, Code: "PUBLIC", Message: "public"}}})
				valid := status >= 400 && status <= 599 && status != 401 && status != 405 && status != 407 && status != 426
				if (err == nil) != valid {
					t.Fatalf("status %d: %v", status, err)
				}
				if !valid {
					var typed *router.IngressConfigurationError
					if !errors.As(err, &typed) || typed.Field != "rejection.status" {
						t.Fatalf("uninspectable: %v", err)
					}
				}
			}
			// Config returns aliased slices on both pins. Preflight checks current native
			// metadata rather than assuming defaults; restore before native dispatch.
			original := methods[0]
			methods[0] = "BAD TOKEN"
			if err := validator.ValidateBoundedIngress(router.BoundedIngressOptions{}); !errors.Is(err, router.ErrIngressConfiguration) {
				t.Fatalf("invalid metadata accepted: %v", err)
			}
			methods[0] = original
			if err := validator.ValidateBoundedIngress(router.BoundedIngressOptions{}); err != nil {
				t.Fatalf("failed preflight poisoned valid retry: %v", err)
			}
			register() // Preflight must not seal the app.
			req, err := http.NewRequest(http.MethodGet, "http://example.com/legacy", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := request(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || string(body) != "legacy" || response.Header.Get("X-Request-ID") != "" {
				t.Fatalf("preflight changed legacy response: %d %q %v", response.StatusCode, body, response.Header)
			}
			if calls != 0 {
				t.Fatalf("preflight invoked callbacks: %d", calls)
			}
		})
	}
}

// Exercise real exported callback contracts directly, not an invented ingress
// runner. T17 must supply these views at the actual admission/drain boundary.
func TestBoundedIngressCallbackCancellation(t *testing.T) {
	type privateKey struct{}
	const secret = "private-context-and-cancellation-cause"
	parent := context.WithValue(context.Background(), privateKey{}, secret)
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	signal := router.IngressCancellationFromContext(ctx)
	ready := make(chan struct{}, 2)
	results := make(chan error, 2)
	finished := make(chan struct{}, 2)
	observer := router.IngressObserver(func(c router.IngressCancellation, event router.IngressEvent) error {
		ready <- struct{}{}
		<-c.Done()
		return c.Err()
	})
	diagnostic := router.IngressDiagnosticHandler(func(c router.IngressCancellation, diagnostic router.IngressDiagnostic) {
		ready <- struct{}{}
		<-c.Done()
		results <- c.Err()
	})
	go func() { defer func() { finished <- struct{}{} }(); results <- observer(signal, router.IngressEvent{}) }()
	go func() { defer func() { finished <- struct{}{} }(); diagnostic(signal, router.IngressDiagnostic{}) }()
	defer func() {
		cancel(nil)
		for i := 0; i < 2; i++ {
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				t.Error("callback did not terminate")
			}
		}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-time.After(2 * time.Second):
			t.Fatal("callback did not start")
		}
	}
	if signal.Err() != nil {
		t.Fatal("canceled before source")
	}
	cancel(errors.New(secret))
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != context.Canceled {
				t.Fatalf("callback error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("callback missed cancellation")
		}
	}
	if _, ok := any(signal).(context.Context); ok {
		t.Fatal("view exposes context values")
	}
	if _, ok := any(signal).(interface{ Cause() error }); ok {
		t.Fatal("view exposes cause")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", signal, signal), secret) {
		t.Fatal("formatting leaked source context")
	}
}

func TestBoundedIngressCancellationProjection(t *testing.T) {
	for name, c := range map[string]router.IngressCancellation{
		"zero": {}, "nil": router.IngressCancellationFromContext(nil), "background": router.IngressCancellationFromContext(context.Background()),
	} {
		t.Run(name, func(t *testing.T) {
			if c.Done() != nil || c.Err() != nil {
				t.Fatal("unexpected cancellation")
			}
			if d, ok := c.Deadline(); ok || !d.IsZero() {
				t.Fatal("unexpected deadline")
			}
		})
	}
	deadline := time.Now().Add(time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	signal := router.IngressCancellationFromContext(ctx)
	if d, ok := signal.Deadline(); !ok || !d.Equal(deadline) {
		t.Fatal("deadline not preserved")
	}
	if signal.Done() != ctx.Done() {
		t.Fatal("created a separate lifetime")
	}
	copy := signal
	cancel()
	if signal.Err() != context.Canceled || copy.Err() != context.Canceled {
		t.Fatal("copies do not share cancellation")
	}
	expired, cancelExpired := context.WithDeadlineCause(context.Background(), time.Now().Add(-time.Second), errors.New("private deadline cause"))
	defer cancelExpired()
	expiredSignal := router.IngressCancellationFromContext(expired)
	select {
	case <-expiredSignal.Done():
	default:
		t.Fatal("expired deadline not visible")
	}
	if expiredSignal.Err() != context.DeadlineExceeded {
		t.Fatal("deadline error not canonical")
	}
	methods := reflect.TypeOf(signal)
	if methods.NumMethod() != 3 {
		t.Fatalf("unexpected view methods: %v", methods)
	}
	for i := 0; i < methods.NumMethod(); i++ {
		switch methods.Method(i).Name {
		case "Deadline", "Done", "Err":
		default:
			t.Fatal("view exposes unexpected capability")
		}
	}
}

func TestBoundedIngressCallbacksCanceledOnEntry(t *testing.T) {
	ctx, cancel := context.WithDeadlineCause(context.Background(), time.Now().Add(-time.Second), errors.New("private cause"))
	defer cancel()
	view := router.IngressCancellationFromContext(ctx)
	calls := 0
	check := func(c router.IngressCancellation) {
		calls++
		select {
		case <-c.Done():
		default:
			t.Error("callback entered without cancellation")
		}
		if c.Err() != context.DeadlineExceeded {
			t.Error("callback lost deadline classification")
		}
	}
	options, err := (router.BoundedIngressOptions{
		Observer:   func(c router.IngressCancellation, _ router.IngressEvent) error { check(c); return c.Err() },
		Diagnostic: func(c router.IngressCancellation, _ router.IngressDiagnostic) { check(c) },
	}).Validated()
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("validation executed callbacks")
	}
	if err := options.Observer(view, router.IngressEvent{}); err != context.DeadlineExceeded {
		t.Fatal("observer result changed")
	}
	options.Diagnostic(view, router.IngressDiagnostic{})
	if calls != 2 {
		t.Fatalf("callback count = %d", calls)
	}
}

func TestBoundedIngressExternalActivation(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, handler := range []router.UnknownMethodHandler{consumerUnknownMethodHandler{}, router.UnknownMethodHandlerFunc(func(context.Context, router.UnknownMethodRequest) (router.UnknownMethodDecision, error) {
			return router.UnknownMethodDecision{Action: router.DelegateNative}, nil
		})} {
			t.Run(fmt.Sprintf("%s/%T", major, handler), func(t *testing.T) {
				var configure router.BoundedIngressServer
				var initialize router.StartupInitializer
				var request func(*http.Request) (*http.Response, error)
				if major == "v2" {
					s := router.NewFiberAdapter()
					configure = s.(router.BoundedIngressServer)
					initialize = s.(router.StartupInitializer)
					request = func(r *http.Request) (*http.Response, error) { return s.WrappedRouter().Test(r) }
				} else {
					s := router.NewFiberV3Adapter()
					configure = s.(router.BoundedIngressServer)
					initialize = s.(router.StartupInitializer)
					request = func(r *http.Request) (*http.Response, error) { return s.WrappedRouter().Test(r) }
				}
				if err := configure.ConfigureBoundedIngress(router.BoundedIngressOptions{UnknownMethod: router.UnknownMethodOptions{Policy: router.UnknownMethodCustom, Handler: handler}}); err != nil {
					t.Fatal(err)
				}
				if err := initialize.Initialize(); err != nil {
					t.Fatal(err)
				}
				req, _ := http.NewRequest("X-CONSUMER", "http://example.test/", nil)
				res, err := request(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				b, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				if res.Header.Get(router.XRequestID) == "" {
					t.Fatal("missing correlation")
				}
				if _, ok := handler.(consumerUnknownMethodHandler); ok {
					if res.StatusCode != 429 || !strings.Contains(string(b), "SLOW_DOWN") {
						t.Fatal(res.StatusCode, string(b))
					}
				}
			})
		}
	}
}
