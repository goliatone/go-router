package flash_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/flash"
	flashMiddleware "github.com/goliatone/go-router/middleware/flash"
)

type mockViewEngine struct {
	renderFunc func(io.Writer, string, any, ...string) error
}

func (m *mockViewEngine) Load() error {
	return nil
}

func (m *mockViewEngine) Render(w io.Writer, name string, bind any, layouts ...string) error {
	return m.renderFunc(w, name, bind, layouts...)
}

func TestFlashMiddleware(t *testing.T) {
	mockEngine := &mockViewEngine{
		renderFunc: func(w io.Writer, name string, bind any, layout ...string) error {
			var data map[string]any
			switch v := bind.(type) {
			case map[string]any:
				data = v
			case fiber.Map:
				data = map[string]any(v)
			default:
				t.Errorf("Expected bind data to be map[string]any, got %T", bind)
				return nil
			}
			return json.NewEncoder(w).Encode(data)
		},
	}

	adapter := router.NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New(fiber.Config{
			UnescapePath:      true,
			EnablePrintRoutes: true,
			StrictRouting:     false,
			PassLocalsToViews: true,
			Views:             mockEngine,
		})
	})

	r := adapter.Router()

	// Apply flash middleware
	r.Use(flashMiddleware.New())

	// POST handler for setting flash
	r.Post("/test", func(ctx router.Context) error {
		var payload struct {
			Name string `json:"name"`
		}
		if err := ctx.Bind(&payload); err != nil {
			return flash.WithError(ctx, router.ViewContext{
				"error_message":  err.Error(),
				"system_message": "Error parsing body",
			}).Status(400).Render("register", router.ViewContext{
				"errors": map[string]string{"form": "Failed to parse form"},
				"record": payload,
			})
		}
		return ctx.JSON(200, payload)
	})

	// GET handler for checking flash data from middleware
	r.Get("/test", func(ctx router.Context) error {
		// Get flash data injected by middleware
		flashData := ctx.Locals("flash").(router.ViewContext)
		return ctx.JSON(200, flashData)
	})

	app := adapter.WrappedRouter()

	tests := []struct {
		name       string
		payload    string
		wantStatus int
		checkFlash bool
	}{
		{
			name:       "Invalid JSON sets flash",
			payload:    `{"name": invalid}`,
			wantStatus: 400,
			checkFlash: true,
		},
		{
			name:       "Valid JSON no flash",
			payload:    `{"name": "test"}`,
			wantStatus: 200,
			checkFlash: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First request to set flash
			req := httptest.NewRequest("POST", "/test", strings.NewReader(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Want status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

				if tt.checkFlash {
					var flashCookie *http.Cookie
					for _, c := range resp.Cookies() {
						if c.Name == "router-app-flash" {
							flashCookie = c
							break
						}
					}
					if flashCookie == nil || flashCookie.Value == "" {
						t.Error("Expected flash cookie to be set")
					}

					// Second request to check flash data from middleware
					req = httptest.NewRequest("GET", "/test", nil)
					req.AddCookie(&http.Cookie{Name: flashCookie.Name, Value: flashCookie.Value})
					resp, err = app.Test(req)
					if err != nil {
						t.Fatalf("Flash check request failed: %v", err)
					}

				// Parse rendered data
				var data router.ViewContext
				if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				// Verify flash data was injected by middleware
				if _, ok := data["error_message"]; !ok {
					t.Error("Expected error_message in flash data from middleware")
				}
				if _, ok := data["system_message"]; !ok {
					t.Error("Expected system_message in flash data from middleware")
				}
			}
		})
	}
}

func TestFlashMiddlewareCustomConfig(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	// Custom flash instance
	customFlash := flash.New(flash.Config{
		Name: "custom-flash",
	})

	// Apply flash middleware with custom config
	r.Use(flashMiddleware.New(flashMiddleware.Config{
		ContextKey: "custom_flash",
		Flash:      customFlash,
	}))

	// Handler to check custom flash data
	r.Get("/custom", func(ctx router.Context) error {
		flashData := ctx.Locals("custom_flash").(router.ViewContext)
		return ctx.JSON(200, map[string]any{
			"flash": flashData,
		})
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/custom", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify custom flash key exists (should be empty but present)
	if _, ok := data["flash"]; !ok {
		t.Error("Expected flash data to be injected with custom key")
	}
}

func TestFlashMiddlewareSkip(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	// Apply flash middleware with skip function
	r.Use(flashMiddleware.New(flashMiddleware.Config{
		Skip: func(c router.Context) bool {
			return c.Path() == "/skip"
		},
	}))

	// Handler that would normally get flash data
	r.Get("/skip", func(ctx router.Context) error {
		flashData := ctx.Locals("flash")
		if flashData != nil {
			t.Error("Expected flash data to be nil when skipped")
		}
		return ctx.JSON(200, map[string]string{"status": "skipped"})
	})

	r.Get("/normal", func(ctx router.Context) error {
		flashData := ctx.Locals("flash")
		if flashData == nil {
			t.Error("Expected flash data to be injected when not skipped")
		}
		return ctx.JSON(200, map[string]string{"status": "normal"})
	})

	app := adapter.WrappedRouter()

	// Test skipped path
	req := httptest.NewRequest("GET", "/skip", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}

	// Test normal path
	req = httptest.NewRequest("GET", "/normal", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Want status 200, got %d", resp.StatusCode)
	}
}

func TestFlashMiddleware_HTTPServer_Injection(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	r.Use(flashMiddleware.New())

	r.Get("/set", func(ctx router.Context) error {
		flash.WithData(ctx, router.ViewContext{"hello": "world"})
		return ctx.SendStatus(200)
	})

	r.Get("/get", func(ctx router.Context) error {
		flashData := ctx.Locals("flash").(router.ViewContext)
		return ctx.JSON(200, flashData)
	})

	h := adapter.WrappedRouter()

	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/set", nil)
	h.ServeHTTP(rr1, req1)
	resp1 := rr1.Result()
	defer resp1.Body.Close()

	var flashCookie *http.Cookie
	for _, c := range resp1.Cookies() {
		if c.Name == "router-app-flash" {
			flashCookie = c
			break
		}
	}
	if flashCookie == nil || flashCookie.Value == "" {
		t.Fatalf("expected flash cookie to be set")
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/get", nil)
	req2.AddCookie(&http.Cookie{Name: flashCookie.Name, Value: flashCookie.Value})
	h.ServeHTTP(rr2, req2)
	resp2 := rr2.Result()
	defer resp2.Body.Close()

	var out router.ViewContext
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["hello"] != "world" {
		t.Fatalf("expected injected flash data, got %v", out)
	}
}
