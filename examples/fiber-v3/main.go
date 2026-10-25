// Run with: go run ./examples/fiber-v3 -address 127.0.0.1:8080
package main

import (
	"context"
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

func main() {
	address := flag.String("address", "127.0.0.1:8080", "listen address")
	flag.Parse()
	host, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := router.NewFiberV3AdapterWithConfig(router.FiberV3AdapterConfig{ListenConfig: fiber3.ListenConfig{ListenerNetwork: "tcp4", DisableStartupMessage: true}})
	r := server.Router()
	r.Use(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error { c.SetContext(host); return next(c) }
	})
	r.Use(router.MiddlewareFromFiberV3(func(c fiber3.Ctx) error { c.Set("X-Backend", "fiber-v3"); return c.Next() }))
	r.Get("/health", func(c router.Context) error { return c.JSON(200, map[string]string{"status": "ok"}) })
	stream := eventstream.New()
	if err := ssefiber.Mount(r, ssefiber.WithStream(stream), ssefiber.WithScopeResolver(func(router.Context) (eventstream.Scope, error) { return eventstream.Scope{"demo": "public"}, nil })); err != nil {
		log.Fatal(err)
	}

	// Hosts own upgraded readers; native Shutdown does not drain hijacked sockets.
	var mu sync.Mutex
	var readers sync.WaitGroup
	active := make(map[router.WebSocketReadInterrupter]struct{})
	stopping := false
	r.WebSocket("/echo", router.DefaultWebSocketConfig(), func(ws router.WebSocketContext) error {
		reader := ws.(router.WebSocketReadInterrupter)
		mu.Lock()
		if stopping {
			mu.Unlock()
			return ws.Close()
		}
		active[reader] = struct{}{}
		readers.Add(1)
		mu.Unlock()
		defer func() { mu.Lock(); delete(active, reader); mu.Unlock(); readers.Done() }()
		defer ws.Close()
		for {
			kind, data, err := ws.ReadMessage()
			if err != nil {
				return err
			}
			if err := ws.WriteMessage(kind, data); err != nil {
				return err
			}
		}
	})
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(*address) }()
	log.Printf("HTTP /health, WebSocket /echo and SSE /events at %s", *address)
	serveReturned := false
	select {
	case <-host.Done():
	case err := <-serveDone:
		serveReturned = true
		log.Printf("serve: %v", err)
		stop()
	}
	mu.Lock()
	stopping = true
	for reader := range active {
		_ = reader.InterruptRead()
	}
	mu.Unlock()
	joined := make(chan struct{})
	go func() { readers.Wait(); close(joined) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-joined:
	case <-ctx.Done():
		log.Printf("reader teardown: %v", ctx.Err())
	}
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	if !serveReturned {
		select {
		case err := <-serveDone:
			if err != nil {
				log.Printf("serve: %v", err)
			}
		case <-ctx.Done():
			log.Printf("serve exit: %v", ctx.Err())
		}
	}
}
