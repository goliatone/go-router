package router_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	fiber2 "github.com/gofiber/fiber/v2"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

func TestDualFiberViewEnginePairs(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		for _, embedded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/embed=%t", backend, embedded), func(t *testing.T) {
				sources := fstest.MapFS{
					"base.html":   {Data: []byte(`BASE:{% block content %}{% endblock %}`)},
					"page.html":   {Data: []byte(`{% extends "base.html" %}{% block content %}{{ title }}:{{ local }}:{{ css("main.css")|safe }}{% endblock %}`)},
					"layout.html": {Data: []byte(`LAYOUT[{{ embed|safe }}]`)},
				}
				override := fstest.MapFS{"base.html": {Data: []byte(`OVERRIDE:{% block content %}{% endblock %}`)}}
				assets := fstest.MapFS{"css/main.css": {Data: []byte("body{}")}}
				dir := t.TempDir()
				for name, file := range sources {
					if err := os.WriteFile(filepath.Join(dir, name), file.Data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				assetDir := t.TempDir()
				if err := os.Mkdir(filepath.Join(assetDir, "css"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(assetDir, "css/main.css"), []byte("body{}"), 0600); err != nil {
					t.Fatal(err)
				}
				cfg := &MockViewConfig{Embed: embedded, DirOS: dir, DirFS: ".", Ext: ".html", CSSPath: "css", AssetsDir: assetDir, URLPrefix: "assets", Reload: true}
				if embedded {
					cfg.TemplateFS = []fs.FS{override, sources}
					cfg.AssetFS = assets
					cfg.AssetsDir = "."
				}
				var views router.Views
				var err error
				if backend == "v3" {
					views, err = router.InitializeFiberV3ViewEngine(cfg)
				} else {
					views, err = router.InitializeViewEngine(cfg)
				}
				if err != nil {
					t.Fatal(err)
				}
				handler := func(c router.Context) error {
					c.Locals("title", "private-local")
					c.Locals("local", "from-local")
					renderer, ok := router.AsTemplateRenderer(c)
					if !ok {
						return fmt.Errorf("renderer missing")
					}
					data, err := renderer.RenderToBytes("page", router.ViewContext{"title": "view"}, "layout")
					if err != nil {
						return err
					}
					state, _ := router.AsResponseState(c)
					if state.ResponseWritten() {
						return fmt.Errorf("buffered render committed")
					}
					return c.Send(data)
				}
				var body string
				var code int
				if backend == "v3" {
					s := router.NewFiberV3Adapter(func(*fiber3.App) *fiber3.App { return fiber3.New(fiber3.Config{Views: views, PassLocalsToViews: true}) })
					s.Router().Get("/view", handler)
					code, body, _ = v3Response(t, s.WrappedRouter(), "GET", "/view", "")
				} else {
					s := router.NewFiberAdapter(func(*fiber2.App) *fiber2.App { return fiber2.New(fiber2.Config{Views: views, PassLocalsToViews: true}) })
					s.Router().Get("/view", handler)
					code, body, err = serveFiberMiddlewareRequest(s.WrappedRouter(), httptest.NewRequest("GET", "/view", nil))
					if err != nil {
						t.Fatal(err)
					}
				}
				prefix := "BASE"
				if embedded {
					prefix = "OVERRIDE"
				}
				if code != 200 || !strings.Contains(body, "LAYOUT["+prefix+":view:from-local:") || !strings.Contains(body, "/assets/css/main.css") || strings.Contains(body, "private-local") {
					t.Fatalf("%d %s", code, body)
				}
				// Disk reload remains active for both engine pairs.
				if !embedded {
					if err := os.WriteFile(filepath.Join(dir, "base.html"), []byte(`RELOADED:{% block content %}{% endblock %}`), 0600); err != nil {
						t.Fatal(err)
					}
					var out bytes.Buffer
					if err := views.Render(&out, "page", map[string]any{"title": "again"}); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(out.String(), "RELOADED") {
						t.Fatal(out.String())
					}
				}
			})
		}
	}
}
