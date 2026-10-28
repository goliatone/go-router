package router_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/goliatone/go-router"
)

// A separate process reproduces the production signal coordinator. A channel
// from the request handler to Shutdown would mask unsynchronized publication.
func TestHTTPServerSignalLifecycle(t *testing.T) {
	if mode := os.Getenv("ROUTER_HTTP_LIFECYCLE_CASE"); mode != "" {
		runHTTPServerSignalChild(t, mode)
		return
	}
	for _, mode := range []string{"drain", "deadline-retry"} {
		t.Run(mode, func(t *testing.T) {
			address, dir := lifecycleAddress(t), t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHTTPServerSignalLifecycle$")
			cmd.Env = append(os.Environ(), "ROUTER_HTTP_LIFECYCLE_CASE="+mode, "ROUTER_HTTP_ADDRESS="+address, "ROUTER_HTTP_MARKERS="+dir)
			log, err := os.Create(filepath.Join(dir, "child.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			t.Cleanup(func() {
				_ = os.WriteFile(filepath.Join(dir, "release"), nil, 0o600)
				cancel()
				<-done
			})
			client := &http.Client{Timeout: 3 * time.Second}
			defer client.CloseIdleConnections()
			until := time.Now().Add(3 * time.Second)
			for {
				resp, err := client.Get("http://" + address + "/ready")
				if err == nil {
					_, readErr := io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if readErr == nil && resp.StatusCode == http.StatusOK {
						break
					}
				}
				if time.Now().After(until) {
					t.Fatal("server did not become ready")
				}
				time.Sleep(time.Millisecond)
			}
			response := make(chan error, 1)
			go func() {
				resp, err := client.Get("http://" + address + "/held")
				if err == nil {
					body, readErr := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if readErr != nil || resp.StatusCode != http.StatusOK || string(body) != "drained" {
						err = fmt.Errorf("held response: %d %q %v", resp.StatusCode, body, readErr)
					}
				}
				response <- err
			}()
			waitHTTPMarker(t, dir, "entered")
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			marker := "stopping"
			if mode == "deadline-retry" {
				marker = "expired"
			}
			waitHTTPMarker(t, dir, marker)
			select {
			case err := <-response:
				t.Fatalf("request completed before release: %v", err)
			default:
			}
			if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := lifecycleReceive(t, response); err != nil {
				t.Fatal(err)
			}
			err = lifecycleReceive(t, done)
			done <- err // Keep cleanup responsible for joining on every path.
			if err != nil {
				out, _ := os.ReadFile(filepath.Join(dir, "child.log"))
				t.Fatalf("signal lifecycle failed: %v\n%s", err, out)
			}
		})
	}
}

func waitHTTPMarker(t *testing.T, dir, name string) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return
		}
		if time.Now().After(until) {
			t.Fatalf("missing lifecycle marker %s", name)
		}
		time.Sleep(time.Millisecond)
	}
}

func runHTTPServerSignalChild(t *testing.T, mode string) {
	host, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	dir := os.Getenv("ROUTER_HTTP_MARKERS")
	server := router.NewHTTPServer()
	// Preserve the existing no-op behavior before Serve.
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.Router().Get("/ready", func(c router.Context) error { return c.SendString("ready") })
	server.Router().Get("/held", func(c router.Context) error {
		if err := os.WriteFile(filepath.Join(dir, "entered"), nil, 0o600); err != nil {
			return err
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
				return c.SendString("drained")
			}
			time.Sleep(time.Millisecond)
		}
	})
	served := make(chan error, 1)
	go func() { served <- server.Serve(os.Getenv("ROUTER_HTTP_ADDRESS")) }()
	select {
	case err := <-served:
		t.Fatalf("unexpected startup exit: %v", err)
	case <-host.Done():
	}
	if err := os.WriteFile(filepath.Join(dir, "stopping"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	timeout := 2 * time.Second
	if mode == "deadline-retry" {
		timeout = 80 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := server.Shutdown(ctx)
	cancel()
	if mode == "deadline-retry" {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected shutdown deadline: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "expired"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		retry, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = server.Shutdown(retry)
		cancel()
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycleReceive(t, served); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve exit: %v", err)
	}
}
