package router_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fiber2 "github.com/gofiber/fiber/v2"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

type compatibilityViews struct{}

func (compatibilityViews) Load() error { return nil }
func (compatibilityViews) Render(w io.Writer, name string, bind any, layouts ...string) error {
	data, err := router.SerializeAsContext(bind)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s:%v", name, data["title"])
	return err
}

type compatibilityServer struct {
	server  lifecycleServer
	name    string
	address string
	done    chan error
}

func mountCompatibility[T any](t *testing.T, name string, s router.Server[T]) compatibilityServer {
	t.Helper()
	r := s.Router()
	r.Use(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			c.SetContext(context.WithValue(c.Context(), middlewareContextKey("matrix"), name))
			return next(c)
		}
	})
	r.Get("/ready", func(c router.Context) error { return c.SendString("ready") })
	r.Group("/api").Get("/items/:id", func(c router.Context) error {
		factory := router.GetWebSocketFactoryByContext(c)
		if factory == nil || factory.AdapterName() != name || c.Context().Value(middlewareContextKey("matrix")) != name || c.RouteName() != "matrix-item" || c.RouteParams()["id"] != "42" {
			return fmt.Errorf("context/factory mismatch")
		}
		return c.SendString("item=" + c.Param("id"))
	}).SetName("matrix-item")
	r.Head("/head", func(c router.Context) error { c.SetHeader("X-Head", "explicit"); return c.NoContent(204) })
	r.Get("/render", func(c router.Context) error {
		renderer, ok := router.AsTemplateRenderer(c)
		if !ok {
			return fmt.Errorf("render capability missing")
		}
		body, err := renderer.RenderToBytes("page", router.ViewContext{"title": "matrix"})
		if err != nil {
			return err
		}
		state, ok := router.AsResponseState(c)
		if !ok || state.ResponseWritten() {
			return fmt.Errorf("render committed")
		}
		captured, err := router.CaptureResponse(c, 1024, func(target router.Context) error { return target.JSON(202, map[string]string{"render": string(body)}) })
		if err != nil {
			return err
		}
		return router.ReplayCapturedResponse(c, captured)
	})
	r.Get("/download", func(c router.Context) error {
		return c.(router.DownloadResponder).WriteStream(c.Context(), "text/plain", strings.NewReader("downloaded"))
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("static"), 0600); err != nil {
		t.Fatal(err)
	}
	r.Static("/assets", dir)
	config := router.DefaultWebSocketConfig()
	config.DisableKeepAlive = true
	echo := func(ws router.WebSocketContext) error {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		return ws.WriteMessage(kind, data)
	}
	r.WebSocket("/ws", config, echo)
	r.Get("/ws-middleware", func(c router.Context) error { return echo(c.(router.WebSocketContext)) }, router.WebSocketUpgrade(config))
	return compatibilityServer{server: s, name: name, address: lifecycleAddress(t), done: make(chan error, 1)}
}

// This test is a required CI gate. Keep the expected backend list explicit so
// deleting an entry cannot silently reduce coexistence coverage.
func TestAdapterCompatibilityMatrix(t *testing.T) {
	expected := []string{"fiber", "fiber-v3", "httprouter"}
	v2 := router.NewFiberAdapter(func(*fiber2.App) *fiber2.App {
		return fiber2.New(fiber2.Config{Views: compatibilityViews{}, PassLocalsToViews: true})
	})
	v3 := router.NewFiberV3Adapter(func(*fiber3.App) *fiber3.App {
		return fiber3.New(fiber3.Config{Views: compatibilityViews{}, PassLocalsToViews: true})
	})
	httpServer := router.NewCompatibilityHTTPServer(compatibilityViews{})
	servers := []compatibilityServer{mountCompatibility(t, "fiber", v2), mountCompatibility(t, "fiber-v3", v3), mountCompatibility(t, "httprouter", httpServer)}
	if len(servers) != len(expected) {
		t.Fatal("required backend missing")
	}
	for i, s := range servers {
		if s.name != expected[i] {
			t.Fatalf("backend %d is %s, want %s", i, s.name, expected[i])
		}
	}
	// All listeners stay live until every backend has served HTTP and both WS paths.
	for i := range servers {
		s := &servers[i]
		go func() { s.done <- s.server.Serve(s.address) }()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := s.server.Shutdown(ctx); err != nil {
				t.Error(err)
			}
			if err := lifecycleReceive(t, s.done); err != nil && err != http.ErrServerClosed {
				t.Error(err)
			}
		})
	}
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	for _, s := range servers {
		t.Run(s.name, func(t *testing.T) {
			deadline := time.Now().Add(time.Second)
			for {
				resp, err := client.Get("http://" + s.address + "/ready")
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(err)
				}
				time.Sleep(time.Millisecond)
			}
			for _, tc := range []struct {
				method, path string
				status       int
				body         string
			}{{"GET", "/api/items/42", 200, "item=42"}, {"HEAD", "/head", 204, ""}, {"GET", "/render", 202, `{"render":"page:matrix"}`}, {"GET", "/download", 200, "downloaded"}, {"GET", "/assets/file.txt", 200, "static"}, {"GET", "/missing", 404, ""}} {
				req, err := http.NewRequest(tc.method, "http://"+s.address+tc.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil || resp.StatusCode != tc.status || !strings.Contains(string(body), tc.body) {
					t.Fatalf("%s: %d %s %v", tc.path, resp.StatusCode, body, readErr)
				}
			}
			for _, path := range []string{"/ws", "/ws-middleware"} {
				conn, resp, err := websocket.DefaultDialer.Dial("ws://"+s.address+path, nil)
				closeWebSocketResponse(t, resp)
				if err != nil {
					t.Fatal(err)
				}
				conn.SetReadDeadline(time.Now().Add(time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, []byte(s.name)); err != nil {
					conn.Close()
					t.Fatal(err)
				}
				_, body, err := conn.ReadMessage()
				conn.Close()
				if err != nil || string(body) != s.name {
					t.Fatalf("%s: %s %v", path, body, err)
				}
			}
		})
	}
}
