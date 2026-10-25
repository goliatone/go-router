package router_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fiber3 "github.com/gofiber/fiber/v3"
	"github.com/goliatone/go-router"
)

func v3Response(t *testing.T, app *fiber3.App, method, path, body string) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "name=test")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWebSocketResponse(t, resp)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(data), resp.Header
}

func TestFiberV3HTTPContracts(t *testing.T) {
	s := router.NewFiberV3Adapter(func(a *fiber3.App) *fiber3.App { return a })
	r := s.Router()
	group := r.Group("/api")
	group.Get("/:id", func(c router.Context) error { return c.SendString("id=" + c.Param("id")) }).SetName("item")
	group.Get("/fixed", func(c router.Context) error { return c.SendString("fixed") })
	group.Head("/fixed", func(c router.Context) error { c.SetHeader("X-Explicit-Head", "yes"); return c.NoContent(204) })
	group.Get("/redirect", func(c router.Context) error { return c.Redirect("/target") })
	group.Get("/route-redirect", func(c router.Context) error { return c.RedirectToRoute("item", router.ViewContext{"id": "7"}) })
	group.Get("/params/:id", func(c router.Context) error {
		if c.RouteName() != "params" || c.RouteParams()["id"] != "42" || c.ParamsInt("id", 0) != 42 || c.QueryInt("n", 0) != 9 || len(c.QueryValues("q")) != 2 {
			return errors.New("route metadata mismatch")
		}
		var cookie struct {
			Name string `cookie:"name"`
		}
		if err := c.CookieParser(&cookie); err != nil || cookie.Name != "test" {
			return fmt.Errorf("cookie: %w", err)
		}
		c.Cookie(&router.Cookie{Name: "result", Value: "yes", HTTPOnly: true})
		return c.SendString("metadata")
	}).SetName("params")
	group.Post("/bind", func(c router.Context) error {
		var payload struct {
			Name string `json:"name"`
		}
		if err := c.Bind(&payload); err != nil {
			return c.SendStatus(400)
		}
		return c.JSON(201, payload)
	})
	group.Get("/capture", func(c router.Context) error {
		captured, err := router.CaptureResponse(c, 1024, func(cc router.Context) error {
			cc.SetHeader("X-Capture", "yes")
			return cc.JSON(202, map[string]string{"name": "captured"})
		})
		if err != nil {
			return err
		}
		state, _ := router.AsResponseState(c)
		if state.ResponseWritten() {
			return errors.New("capture committed live response")
		}
		return router.ReplayCapturedResponse(c, captured)
	})
	group.Get("/download", func(c router.Context) error {
		writer, ok := c.(router.DownloadResponder)
		if !ok {
			return errors.New("download capability missing")
		}
		return writer.WriteStream(context.Background(), "text/plain", strings.NewReader("streamed"))
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "asset.txt"), []byte("asset"), 0600); err != nil {
		t.Fatal(err)
	}
	r.Static("/assets", dir)
	app := s.WrappedRouter()
	s.Init()
	s.Init()
	for _, tc := range []struct {
		method, path, body string
		status             int
		contains           string
	}{
		{"GET", "/api/fixed", "", 200, "fixed"}, {"GET", "/api/abc", "", 200, "id=abc"},
		{"HEAD", "/api/fixed", "", 204, ""}, {"GET", "/api/redirect", "", 302, ""},
		{"GET", "/api/route-redirect", "", 302, ""}, {"GET", "/api/params/42?n=9&q=a&q=b", "", 200, "metadata"},
		{"POST", "/api/bind", `{"name":"bound"}`, 201, "bound"}, {"POST", "/api/bind", `{`, 400, ""},
		{"GET", "/api/capture", "", 202, "captured"}, {"GET", "/api/download", "", 200, "streamed"},
		{"GET", "/assets/asset.txt", "", 200, "asset"}, {"GET", "/absent", "", 404, ""},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			code, body, header := v3Response(t, app, tc.method, tc.path, tc.body)
			if code != tc.status || !strings.Contains(body, tc.contains) {
				t.Fatalf("status=%d body=%s", code, body)
			}
			if tc.path == "/api/redirect" && header.Get("Location") != "/target" {
				t.Fatal(header)
			}
			if tc.path == "/api/route-redirect" && header.Get("Location") != "/api/7" {
				t.Fatal(header)
			}
			if tc.method == "HEAD" && header.Get("X-Explicit-Head") != "yes" {
				t.Fatal("explicit HEAD shadowed")
			}
		})
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("late registration was not rejected")
			}
		}()
		r.Get("/late", func(c router.Context) error { return nil })
	}()
}

func TestFiberV3ServeListenConfigAndNativeStartup(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			address := make(chan net.Addr, 1)
			before := make(chan struct{}, 1)
			cfg := fiber3.ListenConfig{ListenerNetwork: "tcp4", DisableStartupMessage: true, ListenerAddrFunc: func(a net.Addr) { address <- a }, BeforeServeFunc: func(*fiber3.App) error { before <- struct{}{}; return nil }}
			s := router.NewFiberV3AdapterWithConfig(router.FiberV3AdapterConfig{ListenConfig: cfg}, func(a *fiber3.App) *fiber3.App { return a })
			s.Router().Get("/ready", func(c router.Context) error { return c.SendString("ready") })
			done := make(chan error, 1)
			if native {
				app := s.WrappedRouter()
				go func() { done <- app.Listen("127.0.0.1:0", cfg) }()
			} else {
				go func() { done <- s.Serve("127.0.0.1:0") }()
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3e9)
				defer cancel()
				if err := s.Shutdown(ctx); err != nil {
					t.Error(err)
				}
				if err := lifecycleReceive(t, done); err != nil {
					t.Error(err)
				}
			})
			addr := lifecycleReceive(t, address)
			lifecycleReceive(t, before)
			resp, err := testHTTPGet(t, http.DefaultClient, "http://"+addr.String()+"/ready")
			if err != nil {
				t.Fatal(err)
			}
			defer closeWebSocketResponse(t, resp)
			data, err := io.ReadAll(resp.Body)
			if err != nil || string(data) != "ready" || resp.StatusCode != http.StatusOK {
				t.Fatalf("%s %v", data, err)
			}
		})
	}
}

func TestFiberV3ErrorBridge(t *testing.T) {
	s := router.NewFiberV3Adapter(func(a *fiber3.App) *fiber3.App { return a })
	s.Router().Get("/api/denied", func(c router.Context) error { return fmt.Errorf("wrapped: %w", fiber3.NewError(403, "denied")) })
	code, body, _ := v3Response(t, s.WrappedRouter(), "GET", "/api/denied", "")
	if code != 403 || !strings.Contains(body, `"category":"authorization"`) || !strings.Contains(body, `"text_code":"FORBIDDEN"`) || strings.Contains(body, "stack_trace") {
		t.Fatalf("%d %s", code, body)
	}
}
