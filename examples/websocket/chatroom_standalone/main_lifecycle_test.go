//go:build ignore

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

// Run explicitly with main.go, which is an intentionally ignored standalone demo.
func TestChatProcessHelper(t *testing.T) {
	if os.Getenv("H039_CHILD") == "1" {
		main()
		os.Exit(0)
	}
}

func chatAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func chatReady(t *testing.T, address string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	t.Cleanup(client.CloseIdleConnections)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get("http://" + address + "/api/stats")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("host did not become ready")
}

func chatSocket(t *testing.T, address, path string) *websocket.Conn {
	t.Helper()
	c, response, err := websocket.DefaultDialer.Dial("ws://"+address+path, nil)
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func chatRoundtrip(t *testing.T, c *websocket.Conn, marker string) {
	t.Helper()
	if err := c.WriteJSON(map[string]string{"type": "message", "message": marker}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), marker) {
			return
		}
	}
	t.Fatal("chat response missing")
}

func chatClosed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue
		}
		var closed *websocket.CloseError
		if !errors.As(err, &closed) || (closed.Code != 1000 && closed.Code != 1001) {
			t.Fatalf("unclean websocket closure: %v", err)
		}
		return
	}
}

func chatReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("owned work did not finish")
	}
	var zero T
	return zero
}

func chatFactory(high bool) *chatHost {
	if high {
		return newHighLevelChatRoom()
	}
	return newLowLevelChatRoom()
}

func chatCleanup(t *testing.T, h *chatHost) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
}

func TestChatSignalLifecycle(t *testing.T) {
	for _, high := range []bool{false, true} {
		t.Run(fmt.Sprint(high), func(t *testing.T) {
			address := chatAddress(t)
			log, err := os.CreateTemp(t.TempDir(), "host-*.log")
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestChatProcessHelper$")
			cmd.Env = append(os.Environ(), "H039_CHILD=1", "CHAT_ADDR="+address, fmt.Sprintf("USE_HIGH_LEVEL=%t", high))
			cmd.Stdout = log
			cmd.Stderr = log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() { done <- cmd.Wait(); close(finished) }()
			t.Cleanup(func() {
				select {
				case <-finished:
				default:
					_ = cmd.Process.Kill()
					<-finished
				}
			})
			chatReady(t, address)
			client := &http.Client{Timeout: time.Second}
			t.Cleanup(client.CloseIdleConnections)
			page, err := client.Get("http://" + address + "/")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(page.Body)
			page.Body.Close()
			if err != nil || page.StatusCode != 200 || !strings.Contains(string(body), "location.host") {
				t.Fatalf("chat page: status=%d read=%v", page.StatusCode, err)
			}
			c := chatSocket(t, address, "/ws/chat?username=signal")
			chatRoundtrip(t, c, "before-signal")
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			chatClosed(t, c)
			if err := chatReceive(t, done); err != nil {
				data, _ := os.ReadFile(log.Name())
				t.Fatalf("host exit: %v\n%s", err, data)
			}
			data, err := os.ReadFile(log.Name())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "DATA RACE") {
				t.Fatalf("host race:\n%s", data)
			}
		})
	}
}

func TestChatConcurrentClients(t *testing.T) {
	for _, high := range []bool{false, true} {
		t.Run(fmt.Sprint(high), func(t *testing.T) {
			h := chatFactory(high)
			chatCleanup(t, h)
			address := chatAddress(t)
			served := make(chan error, 1)
			go func() { served <- h.Serve(address) }()
			chatReady(t, address)
			var clients sync.WaitGroup
			for i := range 6 {
				clients.Add(1)
				go func() {
					defer clients.Done()
					c := chatSocket(t, address, fmt.Sprintf("/ws/chat?username=client%d", i))
					defer c.Close()
					for j := range 5 {
						chatRoundtrip(t, c, fmt.Sprintf("client%d-message%d", i, j))
					}
				}()
			}
			clients.Wait()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := h.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if err := chatReceive(t, served); !errors.Is(err, http.ErrServerClosed) {
				t.Fatal(err)
			}
			chatReceive(t, h.room.done)
		})
	}
}

func TestChatHostDrainAndRetry(t *testing.T) {
	for _, high := range []bool{false, true} {
		for _, deadline := range []bool{false, true} {
			t.Run(fmt.Sprintf("high=%t/deadline=%t", high, deadline), func(t *testing.T) {
				h := chatFactory(high)
				chatCleanup(t, h)
				entered, release := make(chan struct{}), make(chan struct{})
				unblock := sync.OnceFunc(func() { close(release) })
				t.Cleanup(unblock)
				h.app.Router().Get("/held", func(c router.Context) error { close(entered); <-release; return c.SendString("complete") })
				address := chatAddress(t)
				served := make(chan error, 1)
				go func() { served <- h.Serve(address) }()
				chatReady(t, address)
				c := chatSocket(t, address, "/ws/chat?username=drain")
				chatRoundtrip(t, c, "before-drain")
				response := make(chan error, 1)
				go func() {
					client := &http.Client{Timeout: 4 * time.Second}
					defer client.CloseIdleConnections()
					res, err := client.Get("http://" + address + "/held")
					if err == nil {
						var body []byte
						body, err = io.ReadAll(res.Body)
						res.Body.Close()
						if err == nil && (res.StatusCode != 200 || string(body) != "complete") {
							err = fmt.Errorf("response: %d %q", res.StatusCode, body)
						}
					}
					response <- err
				}()
				chatReceive(t, entered)
				budget := 3 * time.Second
				if deadline {
					budget = 40 * time.Millisecond
				}
				ctx, cancel := context.WithTimeout(context.Background(), budget)
				defer cancel()
				stopped := make(chan error, 1)
				go func() { stopped <- h.Shutdown(ctx) }()
				if deadline {
					if err := chatReceive(t, stopped); !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("deadline result: %v", err)
					}
				} else {
					select {
					case err := <-stopped:
						t.Fatalf("returned before HTTP drain: %v", err)
					case <-time.After(40 * time.Millisecond):
					}
				}
				chatClosed(t, c)
				unblock()
				if err := chatReceive(t, response); err != nil {
					t.Fatal(err)
				}
				if !deadline {
					if err := chatReceive(t, stopped); err != nil {
						t.Fatal(err)
					}
				}
				retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
				defer cancelRetry()
				if err := h.Shutdown(retry); err != nil {
					t.Fatal(err)
				}
				if err := chatReceive(t, served); !errors.Is(err, http.ErrServerClosed) {
					t.Fatalf("serve: %v", err)
				}
				chatReceive(t, h.stopped)
				chatReceive(t, h.room.done)
				if h.room.GetUserCount() != 0 {
					t.Fatal("room clients retained")
				}
			})
		}
	}
}

func TestChatOwnedWorkerDeadline(t *testing.T) {
	for _, high := range []bool{false, true} {
		t.Run(fmt.Sprint(high), func(t *testing.T) {
			h := chatFactory(high)
			chatCleanup(t, h)
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			t.Cleanup(unblock)
			h.app.Router().Get("/ws/held", h.track(func(router.Context) error { close(entered); <-release; return nil }), router.WebSocketUpgrade(router.DefaultWebSocketConfig()))
			address := chatAddress(t)
			served := make(chan error, 1)
			go func() { served <- h.Serve(address) }()
			chatReady(t, address)
			c := chatSocket(t, address, "/ws/held")
			chatReceive(t, entered)
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			if err := h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("worker deadline: %v", err)
			}
			select {
			case <-h.stopped:
				t.Fatal("cleanup finished with an admitted worker held")
			default:
			}
			unblock()
			retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
			defer cancelRetry()
			if err := h.Shutdown(retry); err != nil {
				t.Fatal(err)
			}
			chatClosed(t, c)
			chatReceive(t, served)
			chatReceive(t, h.room.done)
		})
	}
}

func TestChatStartupBoundaries(t *testing.T) {
	for _, high := range []bool{false, true} {
		t.Run(fmt.Sprint(high), func(t *testing.T) {
			t.Run("shutdown-before-serve", func(t *testing.T) {
				h := chatFactory(high)
				chatCleanup(t, h)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := h.Shutdown(ctx); err != nil {
					t.Fatal(err)
				}
				address := chatAddress(t)
				if err := h.Serve(address); !errors.Is(err, http.ErrServerClosed) {
					t.Fatalf("late startup: %v", err)
				}
				conn, err := net.DialTimeout("tcp", address, time.Second)
				if err == nil {
					conn.Close()
					t.Fatal("late listener survived shutdown")
				}
				chatReceive(t, h.room.done)
			})
			t.Run("occupied", func(t *testing.T) {
				l, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				h := chatFactory(high)
				chatCleanup(t, h)
				if err := h.Run(context.Background(), l.Addr().String(), time.Second); err == nil {
					t.Fatal("occupied listener accepted")
				}
				chatReceive(t, h.stopped)
				chatReceive(t, h.room.done)
			})
			t.Run("pre-canceled", func(t *testing.T) {
				h := chatFactory(high)
				chatCleanup(t, h)
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := h.Run(ctx, chatAddress(t), time.Second); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				chatReceive(t, h.stopped)
				chatReceive(t, h.room.done)
			})
			t.Run("startup-cancel", func(t *testing.T) {
				for range 20 {
					h := chatFactory(high)
					ctx, cancel := context.WithCancel(context.Background())
					done := make(chan error, 1)
					address := chatAddress(t)
					go func() { done <- h.Run(ctx, address, time.Second) }()
					cancel()
					if err := chatReceive(t, done); err != nil && !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
					chatReceive(t, h.stopped)
					chatReceive(t, h.room.done)
				}
			})
		})
	}
}
