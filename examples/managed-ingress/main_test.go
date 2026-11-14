package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/stretchr/testify/require"
)

func TestManagedIngressExample(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		for _, policy := range []string{"native", "reject", "custom"} {
			t.Run(backend+"/"+policy, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := run(ctx, config{backend: backend, policy: policy, address: "127.0.0.1:0", smoke: true}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// Keep real lifecycle failures visible even when the parent also cancels.
func TestManagedIngressTerminalFailures(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		t.Run(backend, func(t *testing.T) {
			newServer := func() router.ManagedServer {
				if backend == "v2" {
					s := router.NewFiberAdapter()
					s.Router().Get("/healthz", func(c router.Context) error { return c.SendString("ready") })
					return s.(router.ManagedServer)
				}
				s := router.NewFiberV3Adapter()
				s.Router().Get("/healthz", func(c router.Context) error { return c.SendString("ready") })
				return s.(router.ManagedServer)
			}
			t.Run("forced-drain", func(t *testing.T) {
				var conn net.Conn
				defer func() {
					if conn != nil {
						conn.Close()
					}
				}()
				err := serveManaged(context.Background(), newServer(), router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: 60 * time.Millisecond}, func(host router.ManagedRun) error {
					var err error
					conn, err = net.DialTimeout("tcp", host.Address(), time.Second)
					if err != nil {
						return err
					}
					_, err = io.WriteString(conn, "POST /healthz HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\n\r\nx")
					if err != nil {
						return err
					}
					conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
					_, err = conn.Read(make([]byte, 1))
					if e, ok := err.(net.Error); !ok || !e.Timeout() {
						return fmt.Errorf("expected active incomplete body: %w", err)
					}
					return nil // Parent cancellation now begins drain.
				})
				require.ErrorIs(t, err, router.ErrManagedDrainTimeout)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				conn.SetReadDeadline(time.Now().Add(time.Second))
				_, err = conn.Read(make([]byte, 1))
				require.Error(t, err)
				if e, ok := err.(net.Error); ok {
					require.False(t, e.Timeout(), "owned connection was not closed")
				}
			})
			t.Run("shutdown-failure", func(t *testing.T) {
				cause := errors.New("shutdown failed")
				err := serveManaged(context.Background(), newServer(), router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second, OnShutdown: func(context.Context) error { return cause }}, func(router.ManagedRun) error { return nil })
				require.ErrorIs(t, err, cause)
				require.ErrorIs(t, err, context.Canceled)
			})
			t.Run("early-failure-and-cleanup", func(t *testing.T) {
				checkErr, cleanupErr := errors.New("request failed"), errors.New("cleanup failed")
				err := serveManaged(context.Background(), newServer(), router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second, OnShutdown: func(context.Context) error { return cleanupErr }}, func(router.ManagedRun) error { return checkErr })
				require.ErrorIs(t, err, checkErr)
				require.ErrorIs(t, err, cleanupErr)
			})
			t.Run("canceled-startup-late-failure", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cause := errors.New("late startup failure")
				err := serveManaged(ctx, newServer(), router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second, OnBound: func(context.Context, string) error { cancel(); return cause }}, nil)
				require.ErrorIs(t, err, cause)
			})
		})
	}
}

func TestCancellationOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		clean bool
	}{
		{"parent", errors.Join(context.Canceled), true},
		{"nested-parent", errors.Join(context.Canceled, errors.Join(context.Canceled)), true},
		{"deadline", errors.Join(context.Canceled, context.DeadlineExceeded), false},
		{"hook-canceled", errors.Join(context.Canceled, &router.ManagedError{Phase: "on-shutdown", Err: context.Canceled}), false},
		{"other", errors.Join(context.Canceled, errors.New("failure")), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.clean, cancellationOnly(tc.err)) })
	}
}
