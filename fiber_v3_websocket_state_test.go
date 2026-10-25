package router_test

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func TestFiberV3WebSocketDetachedMiddlewareState(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(fmt.Sprintf("direct=%t", direct), func(t *testing.T) {
			s := router.NewFiberV3Adapter().(*router.FiberV3Adapter)
			type scopeKey struct{}
			s.Router().Use(router.MiddlewareFromFiberV3(func(c fiber3.Ctx) error {
				c.Locals("native-actor", c.Query("actor"))
				return c.Next()
			}))
			s.Router().Use(func(next router.HandlerFunc) router.HandlerFunc {
				return func(c router.Context) error {
					c.Set("actor", c.Query("actor"))
					c.Locals(scopeKey{}, "tenant")
					c.Locals([]byte("buffer"), []byte("owned"))
					return next(c)
				}
			})
			cfg := router.DefaultWebSocketConfig()
			cfg.OnPreUpgrade = func(c router.Context) (router.UpgradeData, error) {
				if c.GetString("actor", "") != "alice" || c.Locals("native-actor") != "alice" {
					return nil, router.ErrUnauthorized
				}
				c.Set("hook-store", "kept")
				c.Locals("hook-local", "kept")
				return router.UpgradeData{"actor": c.GetString("actor", "")}, nil
			}
			handler := func(ws router.WebSocketContext) error {
				// Keep the callback alive while ordinary requests recycle Ctxs.
				if _, _, err := ws.ReadMessage(); err != nil {
					return err
				}
				if ws.GetString("actor", "") != "alice" || ws.GetString("hook-store", "") != "kept" || ws.Locals("native-actor") != "alice" || ws.Locals("hook-local") != "kept" || ws.Locals(scopeKey{}) != "tenant" || string(ws.Locals("buffer").([]byte)) != "owned" {
					return ws.WriteMessage(router.TextMessage, []byte("middleware state lost"))
				}
				if ws.Header("X-Actor") != "alice" || ws.Header("x-actor") != "alice" || ws.Header("X-ACTOR") != "alice" {
					return ws.WriteMessage(router.TextMessage, []byte("header case mismatch"))
				}
				ws.Locals("mutable", "updated")
				ws.LocalsMerge(scopeKey{}, map[string]any{"one": 1})
				merged := ws.LocalsMerge(scopeKey{}, map[string]any{"two": 2})
				if ws.Locals("mutable") != "updated" || merged["one"] != 1 || merged["two"] != 2 {
					return ws.WriteMessage(router.TextMessage, []byte("local mutations lost"))
				}
				var workers sync.WaitGroup
				for i := range 8 {
					workers.Go(func() { ws.Locals(i, i); ws.LocalsMerge("concurrent", map[string]any{fmt.Sprint(i): i}) })
				}
				workers.Wait()
				if len(ws.Locals("concurrent").(map[string]any)) != 8 {
					return ws.WriteMessage(router.TextMessage, []byte("concurrent local merge lost"))
				}
				return ws.WriteMessage(router.TextMessage, []byte("ok"))
			}
			if direct {
				s.Router().WebSocket("/ws", cfg, handler)
			} else {
				s.Router().Get("/ws", func(c router.Context) error { return handler(c.(router.WebSocketContext)) }, router.WebSocketUpgrade(cfg))
			}
			s.Router().Get("/reuse", func(c router.Context) error { return c.SendString("recycled") })
			addr, shutdown := startFiberV3Server(t, s)
			defer shutdown()
			conn, resp, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws?actor=alice", http.Header{"X-Actor": {"alice"}})
			closeWebSocketResponse(t, resp)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			for range 16 {
				resp, err := http.Get("http://" + addr + "/reuse?actor=other")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte("inspect")); err != nil {
				t.Fatal(err)
			}
			_, data, err := conn.ReadMessage()
			if err != nil || string(data) != "ok" {
				t.Fatalf("detached state: %q, %v", data, err)
			}
		})
	}
}
