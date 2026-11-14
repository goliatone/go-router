// managed-ingress demonstrates explicit bounded ingress and owned HTTP shutdown.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	fiber2 "github.com/gofiber/fiber/v2"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

// ApplicationStrategy chooses a global unknown-method response. It has no route
// lookup, response writer, native context or authority to dispatch a request.
type ApplicationStrategy struct{}

func (ApplicationStrategy) HandleUnknownMethod(_ context.Context, request router.UnknownMethodRequest) (router.UnknownMethodDecision, error) {
	if request.Method == "X-NATIVE" {
		return router.UnknownMethodDecision{Action: router.DelegateNative}, nil
	}
	return router.UnknownMethodDecision{Action: router.RejectUnknownMethod, Rejection: router.UnknownMethodRejection{Status: 429, Code: "METHOD_REJECTED", Message: "method rejected"}}, nil
}

type config struct {
	backend, policy, address string
	smoke                    bool
}

func run(parent context.Context, cfg config) error {
	var managed router.ManagedServer
	opts := router.BoundedIngressOptions{}
	switch cfg.policy {
	case "native":
	case "reject":
		opts.UnknownMethod.Policy = router.UnknownMethodReject
	case "custom":
		opts.UnknownMethod = router.UnknownMethodOptions{Policy: router.UnknownMethodCustom, Handler: ApplicationStrategy{}}
	default:
		return fmt.Errorf("unknown policy %q", cfg.policy)
	}
	health := func(c router.Context) error { return c.SendString("ready") }
	switch cfg.backend {
	case "v2":
		app := router.NewFiberAdapter(func(*fiber2.App) *fiber2.App { return fiber2.New(fiber2.Config{DisableStartupMessage: true}) })
		if err := app.(router.BoundedIngressServer).ConfigureBoundedIngress(opts); err != nil {
			return err
		}
		app.Router().Get("/healthz", health)
		managed = app.(router.ManagedServer)
	case "v3":
		app := router.NewFiberV3AdapterWithConfig(router.FiberV3AdapterConfig{ListenConfig: fiber3.ListenConfig{DisableStartupMessage: true}})
		if err := app.(router.BoundedIngressServer).ConfigureBoundedIngress(opts); err != nil {
			return err
		}
		app.Router().Get("/healthz", health)
		managed = app.(router.ManagedServer)
	default:
		return fmt.Errorf("unknown backend %q", cfg.backend)
	}
	var check func(router.ManagedRun) error
	if cfg.smoke {
		check = func(host router.ManagedRun) error { return smoke(host, cfg.policy) }
	}
	// Host-owned SSE/WS resources would be closed/joined in OnShutdown. This
	// example owns only HTTP; managed drain does not close hijacked transports.
	return serveManaged(parent, managed, router.ManagedOptions{Address: cfg.address, DrainTimeout: time.Second}, check)
}

// serveManaged always collects the terminal result, including early startup and
// smoke failures. Parent cancellation is expected; independent failures are not.
func serveManaged(parent context.Context, managed router.ManagedServer, opts router.ManagedOptions, check func(router.ManagedRun) error) (result error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	host, err := managed.StartManaged(ctx, opts)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelCleanup()
		stopErr := host.Stop(cleanup)
		terminalErr := host.Wait(cleanup)
		if terminalErr != nil {
			result = errors.Join(result, terminalErr)
		} else if stopErr != nil {
			result = errors.Join(result, stopErr)
		}
		if ctx.Err() == context.Canceled && cancellationOnly(result) {
			result = nil
		}
	}()
	select {
	case <-host.Serving():
	case <-host.Done():
		return nil // The deferred join reports the complete startup result.
	case <-ctx.Done():
		return nil // Stop and join before reporting cancellation or late failures.
	}
	if check != nil {
		if err := check(host); err != nil {
			return err
		}
		cancel() // Exercises parent cancellation and owned listener cleanup.
	}
	<-host.Done()
	return nil
}

// ManagedRun joins the parent's cancellation sentinel with independent errors.
// Only bare cancellation leaves in join containers are expected. Preserve other
// errors, including phase wrappers, even if their Is method matches Canceled.
func cancellationOnly(err error) bool {
	if err == context.Canceled {
		return true
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return false
	}
	children := joined.Unwrap()
	if len(children) == 0 {
		return false
	}
	for _, child := range children {
		if !cancellationOnly(child) {
			return false
		}
	}
	return true
}

func smoke(host router.ManagedRun, policy string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	for _, method := range []string{"GET", "X-EXAMPLE"} {
		req, err := http.NewRequest(method, "http://"+host.Address()+"/healthz", nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Request-ID", "untrusted")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		_, readErr := io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}
		if id := res.Header.Get("X-Request-ID"); id == "" || id == "untrusted" {
			return errors.New("missing owned correlation")
		}
		if method == "GET" && res.StatusCode != 200 {
			return fmt.Errorf("health status %d", res.StatusCode)
		}
		if method != "GET" && policy != "native" {
			want := 501
			if policy == "custom" {
				want = 429
			}
			if res.StatusCode != want {
				return fmt.Errorf("policy status %d", res.StatusCode)
			}
		}
	}
	return nil
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.backend, "backend", "v3", "v2 or v3 (explicit backend choice)")
	flag.StringVar(&cfg.policy, "policy", "native", "native, reject or custom")
	flag.StringVar(&cfg.address, "address", "127.0.0.1:8080", "HTTP listen address")
	flag.BoolVar(&cfg.smoke, "smoke", false, "exercise HTTP requests, cancel and exit")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
