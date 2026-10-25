package router

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type upgradeStatusError struct{ code int }

func (e upgradeStatusError) Error() string   { return "private status details" }
func (e upgradeStatusError) StatusCode() int { return e.code }

func TestWebSocketPreUpgradeResponses(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		code    int
		handled bool
	}{
		{"unauthorized", NewUnauthorizedError("secret"), 401, false},
		{"forbidden", NewForbiddenError("secret"), 403, false},
		{"unavailable", NewInternalError(errors.New("secret"), "secret").WithCode(503), 503, false},
		{"internal", NewInternalError(errors.New("secret"), "secret"), 500, false},
		{"wrapped status", fmt.Errorf("hidden: %w", upgradeStatusError{429}), 429, false},
		{"legacy", errors.New("secret"), 400, false},
		{"rendered", fmt.Errorf("handled: %w", ErrWebSocketUpgradeHandled), 401, true},
	}
	for _, mode := range []string{"fiber", "http", "fiber-middleware", "http-middleware", "fiber-v3", "fiber-v3-middleware"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				cfg := DefaultWebSocketConfig()
				cfg.OnPreUpgrade = func(c Context) (UpgradeData, error) {
					if tc.handled {
						c.SetHeader("X-Auth-Result", "login-required")
						if err := c.JSON(401, map[string]string{"code": "AUTHENTICATION_REQUIRED"}); err != nil {
							return nil, err
						}
					}
					return nil, tc.err
				}
				called := false
				upgraded := func(WebSocketContext) error { called = true; return nil }
				ordinary := func(Context) error { called = true; return nil }
				req := httptest.NewRequestWithContext(t.Context(), "GET", "http://example.com/ws", nil)
				req.RequestURI = "/ws"
				req.Header.Set("Host", "example.com")
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Sec-WebSocket-Version", "13")
				req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
				var res *http.Response
				if strings.HasPrefix(mode, "fiber-v3") {
					adapter := NewFiberV3Adapter()
					if strings.Contains(mode, "middleware") {
						adapter.Router().Get("/ws", ordinary, WebSocketUpgrade(cfg))
					} else {
						adapter.Router().WebSocket("/ws", cfg, upgraded)
					}
					var err error
					res, err = adapter.WrappedRouter().Test(req)
					if err != nil {
						t.Fatal(err)
					}
				} else if strings.HasPrefix(mode, "fiber") {
					adapter, ok := NewFiberAdapter().(*FiberAdapter)
					if !ok {
						t.Fatal("unexpected Fiber adapter type")
					}
					if strings.Contains(mode, "middleware") {
						adapter.Router().Get("/ws", ordinary, WebSocketUpgrade(cfg))
					} else {
						adapter.Router().WebSocket("/ws", cfg, upgraded)
					}
					var err error
					res, err = adapter.WrappedRouter().Test(req)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					server := NewHTTPServer()
					if strings.Contains(mode, "middleware") {
						server.Router().Get("/ws", ordinary, WebSocketUpgrade(cfg))
					} else {
						server.Router().WebSocket("/ws", cfg, upgraded)
					}
					rec := httptest.NewRecorder()
					server.WrappedRouter().ServeHTTP(rec, req)
					res = rec.Result()
				}
				defer func() {
					if closeErr := res.Body.Close(); closeErr != nil {
						t.Error(closeErr)
					}
				}()
				raw, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != tc.code || called {
					t.Fatalf("status=%d called=%t body=%s", res.StatusCode, called, raw)
				}
				if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "private") {
					t.Fatal("internal details leaked")
				}
				if tc.handled && (res.Header.Get("X-Auth-Result") != "login-required" || !strings.Contains(string(raw), "AUTHENTICATION_REQUIRED")) {
					t.Fatal("rendered response overwritten", string(raw))
				}
			})
		}
	}
}
