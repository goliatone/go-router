package router_test

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

func TestRouter_Static(t *testing.T) {
	// Create test filesystem structure using relative paths
	tempDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"index.html":        "<h1>Index</h1>",
		"style.css":         "body { color: red; }",
		"nested/file.txt":   "Hello from nested file",
		"nested/index.html": "<h1>Nested Index</h1>",
	}

	for fpath, content := range files {
		fullPath := filepath.Join(tempDir, fpath)
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		if err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		err = os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	tests := []struct {
		name        string
		prefix      string
		setupStatic func(router.Router[*fiber.App])
		requestPath string
		method      string
		wantStatus  int
		wantContent string
		wantHeaders map[string]string
	}{
		{
			name:   "Serve index.html",
			prefix: "/public",
			setupStatic: func(r router.Router[*fiber.App]) {
				r.Static("/public", tempDir)
			},
			requestPath: "/public",
			method:      "GET",
			wantStatus:  200,
			wantContent: "<h1>Index</h1>",
		},
		{
			name:   "Serve CSS file",
			prefix: "/public",
			setupStatic: func(r router.Router[*fiber.App]) {
				r.Static("/public", tempDir)
			},
			requestPath: "/public/style.css",
			method:      "GET",
			wantStatus:  200,
			wantContent: "body { color: red; }",
			wantHeaders: map[string]string{
				"Content-Type": "text/css; charset=utf-8",
			},
		},
		{
			name:   "Serve nested file",
			prefix: "/public",
			setupStatic: func(r router.Router[*fiber.App]) {
				r.Static("/public", tempDir)
			},
			requestPath: "/public/nested/file.txt",
			method:      "GET",
			wantStatus:  200,
			wantContent: "Hello from nested file",
		},
		{
			name:   "HEAD request",
			prefix: "/public",
			setupStatic: func(r router.Router[*fiber.App]) {
				r.Static("/public", tempDir)
			},
			requestPath: "/public/style.css",
			method:      "HEAD",
			wantStatus:  200,
			wantHeaders: map[string]string{
				"Content-Type": "text/css; charset=utf-8",
			},
		},
		{
			name:   "File not found",
			prefix: "/public",
			setupStatic: func(r router.Router[*fiber.App]) {
				r.Static("/public", tempDir)
			},
			requestPath: "/public/notfound.txt",
			method:      "GET",
			wantStatus:  404,
		},
		{
			name:   "Custom options - MaxAge",
			prefix: "/assets",
			setupStatic: func(r router.Router[*fiber.App]) {
				r.Static("/assets", tempDir, router.Static{
					MaxAge: 3600,
					Root:   tempDir, // Explicitly set root
				})
			},
			requestPath: "/assets/style.css",
			method:      "GET",
			wantStatus:  200,
			wantHeaders: map[string]string{
				"Cache-Control": "public, max-age=3600",
				"Content-Type":  "text/css; charset=utf-8",
			},
		},
		{
			name:   "Custom options - Download",
			prefix: "/downloads",
			setupStatic: func(r router.Router[*fiber.App]) {
				r.Static("/downloads", tempDir, router.Static{
					Download: true,
					Root:     tempDir, // Explicitly set root
				})
			},
			requestPath: "/downloads/style.css",
			method:      "GET",
			wantStatus:  200,
			wantHeaders: map[string]string{
				"Content-Type":        "text/css; charset=utf-8",
				"Content-Disposition": "attachment; filename=style.css",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test case: %s", tt.name)
			t.Logf("Request path: %s", tt.requestPath)
			t.Logf("Temp dir: %s", tempDir)

			adapter := router.NewFiberAdapter()
			r := adapter.Router()

			tt.setupStatic(r)
			adapter.Init()

			app := adapter.WrappedRouter()

			req := httptest.NewRequest(tt.method, tt.requestPath, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}

			if resp.StatusCode != tt.wantStatus {
				if resp.StatusCode == 404 {
					t.Logf("File not found at path: %s", tt.requestPath)
				}
				t.Errorf("Status = %v, want %v", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantContent != "" && tt.method != "HEAD" {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("Failed to read body: %v", err)
				}
				if string(body) != tt.wantContent {
					t.Errorf("Body = %v, want %v", string(body), tt.wantContent)
				}
			}

			for header, want := range tt.wantHeaders {
				if got := resp.Header.Get(header); got != want {
					t.Errorf("Header %s = %v, want %v", header, got, want)
				}
			}
		})
	}
}
