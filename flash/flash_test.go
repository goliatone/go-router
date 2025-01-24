package flash_test

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/flash"
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
			data, ok := bind.(map[string]any)
			if !ok {
				t.Error("Expected bind data to be ViewContext")
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

	// GET handler for checking flash
	r.Get("/test", func(ctx router.Context) error {
		flashData := flash.Get(ctx)
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
			name:       "Invalid JSON",
			payload:    `{"name": invalid}`,
			wantStatus: 400,
			checkFlash: true,
		},
		{
			name:       "Valid JSON",
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
				cookie := resp.Header.Get("Set-Cookie")
				if cookie == "" {
					t.Error("Expected flash cookie to be set")
				}
				if !strings.Contains(cookie, "router-app-flash") {
					t.Error("Expected cookie name router-app-flash")
				}

				// Second request to check flash
				req = httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Cookie", cookie)
				resp, err = app.Test(req)
				if err != nil {
					t.Fatalf("Flash check request failed: %v", err)
				}

				// Parse rendered data
				var data router.ViewContext
				if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				// Verify flash data
				if _, ok := data["error_message"]; !ok {
					t.Error("Expected error_message in flash data")
				}
				if _, ok := data["system_message"]; !ok {
					t.Error("Expected system_message in flash data")
				}
			}
		})
	}
}
