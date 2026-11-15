// Run with: go run ./examples/fiber-v3 -address 127.0.0.1:8080
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
	"github.com/goliatone/go-router/eventstream"
	"github.com/goliatone/go-router/ssefiber"
)

// exampleHost owns upgraded readers and SSE subscriptions. Managed hosting owns
// the listener, ordinary HTTP drain, and response-stream connection lifetime.
type exampleHost struct {
	server    router.Server[*fiber3.App]
	stream    eventstream.Stream
	mu        sync.Mutex
	stopping  bool
	resources map[*ownedResource]struct{}
	workers   sync.WaitGroup
	joined    chan struct{}
}
type ownedResource struct{ stop func() error }

func newExampleHost() (*exampleHost, error) {
	h := &exampleHost{
		server: router.NewFiberV3AdapterWithConfig(router.FiberV3AdapterConfig{ListenConfig: fiber3.ListenConfig{ListenerNetwork: "tcp4", DisableStartupMessage: true}}),
		stream: eventstream.New(), resources: make(map[*ownedResource]struct{}), joined: make(chan struct{}),
	}
	r := h.server.Router()
	// Retain managed request contexts: parent cancellation must not abort ordinary
	// admitted HTTP work before its drain deadline.
	r.Use(router.MiddlewareFromFiberV3(func(c fiber3.Ctx) error { c.Set("X-Backend", "fiber-v3"); return c.Next() }))
	r.Get("/health", func(c router.Context) error { return c.JSON(200, map[string]string{"status": "ok"}) })
	if err := ssefiber.Mount(r, ssefiber.WithStream(ownedStream{Stream: h.stream, host: h}), ssefiber.WithScopeResolver(func(router.Context) (eventstream.Scope, error) { return eventstream.Scope{"demo": "public"}, nil })); err != nil {
		return nil, err
	}
	r.WebSocket("/echo", router.DefaultWebSocketConfig(), h.echo)
	return h, nil
}

// own serializes admission with shutdown; stop functions run outside the lock.
func (h *exampleHost) own(stop func() error) (func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopping {
		return nil, false
	}
	resource := &ownedResource{stop: stop}
	h.resources[resource] = struct{}{}
	h.workers.Add(1)
	return func() {
		h.mu.Lock()
		delete(h.resources, resource)
		h.mu.Unlock()
		h.workers.Done()
	}, true
}

// ownSocket keeps a shutdown snapshot from interrupting an already-closing
// transport, while still joining its close callbacks before releasing ownership.
func (h *exampleHost) ownSocket(ws router.WebSocketContext) (func(), bool) {
	reader := ws.(router.WebSocketReadInterrupter)
	var mu sync.Mutex
	closing := false
	release, ok := h.own(func() error {
		mu.Lock()
		defer mu.Unlock()
		if closing {
			return nil
		}
		return reader.InterruptRead()
	})
	if !ok {
		return nil, false
	}
	return func() {
		mu.Lock()
		closing = true
		mu.Unlock()
		_ = ws.Close()
		release()
	}, true
}

func (h *exampleHost) echo(ws router.WebSocketContext) error {
	release, ok := h.ownSocket(ws)
	if !ok {
		return ws.Close()
	}
	defer release()
	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		if err := ws.WriteMessage(kind, data); err != nil {
			return err
		}
	}
}

// shutdown is invoked once by ManagedRun. It joins even after the shared drain
// deadline: Stop can report timeout while Done/Wait still represent actual cleanup.
func (h *exampleHost) shutdown(context.Context) error {
	h.mu.Lock()
	h.stopping = true
	resources := make([]*ownedResource, 0, len(h.resources))
	for resource := range h.resources {
		resources = append(resources, resource)
	}
	h.mu.Unlock()
	var result error
	for _, resource := range resources {
		result = errors.Join(result, resource.stop())
	}
	h.workers.Wait()
	close(h.joined)
	return result
}

func (h *exampleHost) start(parent context.Context, options router.ManagedOptions) (router.ManagedRun, error) {
	options.OnShutdown = h.shutdown
	return h.server.(router.ManagedServer).StartManaged(parent, options)
}

// ownedStream joins the subscription's Records channel on cancellation. Its
// forwarding worker owns only immutable records and standard contexts, never a
// pooled request wrapper. The SSE adapter and managed transport own the writer.
type ownedStream struct {
	eventstream.Stream
	host *exampleHost
}

func (s ownedStream) Subscribe(parent context.Context, scope eventstream.Scope, cursor string) (*eventstream.Subscription, error) {
	ctx, cancel := context.WithCancel(parent)
	release, ok := s.host.own(func() error { cancel(); return nil })
	if !ok {
		cancel()
		return nil, errors.New("host is stopping")
	}
	sub, err := s.Stream.Subscribe(ctx, scope, cursor)
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	records := make(chan eventstream.Record)
	go func() {
		defer release()
		defer close(records)
		defer func() {
			cancel()
			for range sub.Records {
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case record, ok := <-sub.Records:
				if !ok {
					return
				}
				select {
				case records <- record:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	copy := *sub
	copy.Records = records
	return &copy, nil
}

func run(parent context.Context, address string) error {
	h, err := newExampleHost()
	if err != nil {
		return err
	}
	lifetime, err := h.start(parent, router.ManagedOptions{Address: address, Network: "tcp4", DrainTimeout: 5 * time.Second})
	if err != nil {
		if parent.Err() == context.Canceled && cancellationOnly(err, context.Cause(parent)) {
			return nil
		}
		return err
	}
	select {
	case <-lifetime.Serving():
		log.Printf("HTTP /health, WebSocket /echo and SSE /events at %s", lifetime.Address())
	case <-lifetime.Done():
	case <-parent.Done():
	}
	select {
	case <-lifetime.Done():
	case <-parent.Done():
	}
	// Parent cancellation already requests stop. Bound this process's final wait;
	// the managed handle retains the eventual sticky result for embedded callers.
	cleanup, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	err = lifetime.Wait(cleanup)
	if parent.Err() == context.Canceled && cancellationOnly(err, context.Cause(parent)) {
		return nil
	}
	return err
}

// Do not suppress independent failures merely because a joined error also
// matches context.Canceled.
func cancellationOnly(err, cause error) bool {
	if err == context.Canceled || (cause != nil && err == cause) {
		return true
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok || len(joined.Unwrap()) == 0 {
		return false
	}
	for _, child := range joined.Unwrap() {
		if !cancellationOnly(child, cause) {
			return false
		}
	}
	return true
}

func main() {
	address := flag.String("address", "127.0.0.1:8080", "listen address")
	flag.Parse()
	host, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(host, *address); err != nil {
		log.Fatal(err)
	}
}
