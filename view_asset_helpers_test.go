package router_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	router "github.com/goliatone/go-router"
)

func TestViewAssetHelpersObserveDebugChanges(t *testing.T) {
	engines := []struct {
		name   string
		create func(*router.SimpleViewConfig) (router.Views, error)
	}{
		{"v2", func(cfg *router.SimpleViewConfig) (router.Views, error) { return router.InitializeViewEngine(cfg) }},
		{"v3", func(cfg *router.SimpleViewConfig) (router.Views, error) {
			return router.InitializeFiberV3ViewEngine(cfg)
		}},
	}
	for _, engine := range engines {
		for _, kind := range []string{"css", "js"} {
			for _, initialDebug := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/initial-debug=%t", engine.name, kind, initialDebug), func(t *testing.T) {
					dir := t.TempDir()
					name := "missing." + kind
					source := fmt.Sprintf(`{{ %s(%q)|safe }}`, kind, name)
					if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte(source), 0600); err != nil {
						t.Fatal(err)
					}
					assets := t.TempDir()
					if err := os.Mkdir(filepath.Join(assets, kind), 0700); err != nil {
						t.Fatal(err)
					}
					cfg := router.NewSimpleViewConfig(dir).WithAssets(assets, kind, kind).
						WithDebug(initialDebug).WithReload(false)
					views, err := engine.create(cfg)
					if err != nil {
						t.Fatal(err)
					}
					for _, debug := range []bool{initialDebug, !initialDebug, initialDebug} {
						cfg.WithDebug(debug)
						var out bytes.Buffer
						if renderErr := views.Render(&out, "page", nil); renderErr != nil {
							t.Fatal(renderErr)
						}
						want := ""
						if debug {
							want = fmt.Sprintf("<!-- %s NOT FOUND: %s (looked in %s) -->", strings.ToUpper(kind), name, kind)
						}
						if out.String() != want {
							t.Fatalf("debug=%t: got %q, want %q", debug, out.String(), want)
						}
					}
				})
			}
		}
	}
}
