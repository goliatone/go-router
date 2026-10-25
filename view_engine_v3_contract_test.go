package router_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	p6 "github.com/flosch/pongo2/v6"
	p7 "github.com/flosch/pongo2/v7"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

type foreignDynamicMap map[string]string

func (foreignDynamicMap) Hidden() any { return p6.AsValue("<script>bad</script>") }

type foreignDynamicModel struct{}

func (foreignDynamicModel) Hidden() any { return p6.AsValue("<script>bad</script>") }

func newContractViews(t *testing.T, source string, funcs map[string]any, reload bool) router.Views {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	views, err := router.DefaultFiberV3ViewEngine(&MockViewConfig{DirOS: dir, Ext: ".html", Functions: funcs, Reload: reload})
	if err != nil {
		t.Fatal(err)
	}
	return views
}

func TestFiberV3ViewForeignValuesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		funcs, data  map[string]any
	}{
		{"unsafe-helper", `{{ echo(input) }}`, map[string]any{"echo": func(s string) *p6.Value { return p6.AsValue(s) }}, map[string]any{"input": "<script>bad</script>"}},
		{"false-helper", `{% if allowed() %}allowed{% else %}denied{% endif %}`, map[string]any{"allowed": func() *p6.Value { return p6.AsValue(false) }}, nil},
		{"typed-argument", `{{ echo(input) }}`, map[string]any{"echo": func(v *p6.Value) *p6.Value { return v }}, map[string]any{"input": "hello"}},
		{"dynamic-helper", `{{ echo() }}`, map[string]any{"echo": func() any { return p6.AsValue("<script>bad</script>") }}, nil},
		{"nested-helper-result", `{{ echo().item }}`, map[string]any{"echo": func() any { return map[string]any{"item": p6.AsSafeValue("<script>bad</script>")} }}, nil},
		{"binding", `{{ input }}`, nil, map[string]any{"input": p6.AsValue("<script>bad</script>")}},
		{"safe-binding", `{{ input }}`, nil, map[string]any{"input": p6.AsSafeValue("<script>bad</script>")}},
		{"nested-false-binding", `{% if input.item %}allowed{% endif %}`, nil, map[string]any{"input": map[string]any{"item": p6.AsValue(false)}}},
		{"boxed-native-container", `{{ input }}`, nil, map[string]any{"input": p7.AsValue(p6.AsValue("<script>bad</script>"))}},
		{"map-method", `{{ input.Hidden() }}`, nil, map[string]any{"input": foreignDynamicMap{}}},
		{"model-method", `{{ input.Hidden() }}`, nil, map[string]any{"input": foreignDynamicModel{}}},
		{"binding-helper", `{{ echo() }}`, nil, map[string]any{"echo": func() any { return p6.AsValue("<script>bad</script>") }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			views := newContractViews(t, tc.source, tc.funcs, false)
			var out bytes.Buffer
			err := views.Render(&out, "page", tc.data)
			if err == nil || !strings.Contains(err.Error(), "migrate helpers/data") {
				t.Fatalf("missing migration error: output=%q err=%v", out.String(), err)
			}
			if strings.Contains(out.String(), "<script>") || strings.Contains(out.String(), "allowed") {
				t.Fatalf("foreign value escaped boundary: %q", out.String())
			}
		})
	}
}

func TestFiberV3ViewNativeValuesAndHelpers(t *testing.T) {
	funcs := map[string]any{
		"unsafe":     func() *p7.Value { return p7.AsValue("<b>unsafe</b>") },
		"safe":       func() *p7.Value { return p7.AsSafeValue("<i>safe</i>") },
		"allowed":    func() *p7.Value { return p7.AsValue(false) },
		"echo":       func(v *p7.Value) *p7.Value { return v },
		"join":       func(parts ...string) string { return strings.Join(parts, ":") },
		"dynamic":    func() any { return "<em>dynamic</em>" },
		"contextual": func(_ *p7.ExecutionContext, s string) (string, error) { return s, nil },
	}
	views := newContractViews(t, `{{ unsafe() }}|{{ safe() }}|{% if allowed() %}bad{% else %}false{% endif %}|{{ echo(input) }}|{{ join("a", "b") }}|{{ dynamic() }}|{{ contextual("ctx") }}`, funcs, true)
	for range 2 {
		var out bytes.Buffer
		if err := views.Render(&out, "page", map[string]any{"input": "<x>"}); err != nil {
			t.Fatal(err)
		}
		want := `&lt;b&gt;unsafe&lt;/b&gt;|<i>safe</i>|false|&lt;x&gt;|a:b|&lt;em&gt;dynamic&lt;/em&gt;|ctx`
		if out.String() != want {
			t.Fatalf("got %q want %q", out.String(), want)
		}
	}
	failed := newContractViews(t, `{{ fail() }}`, map[string]any{"fail": func() (string, error) { return "", errors.New("helper failed") }}, false)
	if err := failed.Render(io.Discard, "page", nil); err == nil || !strings.Contains(err.Error(), "helper failed") {
		t.Fatalf("helper error lost: %v", err)
	}
}

func TestFiberV3ViewForeignValueHTTP(t *testing.T) {
	views := newContractViews(t, `{{ echo(input) }}`, map[string]any{"echo": func(s string) any { return p6.AsValue(s) }}, false)
	adapter := router.NewFiberV3Adapter(func(*fiber3.App) *fiber3.App { return fiber3.New(fiber3.Config{Views: views}) })
	adapter.Router().Get("/page", func(c router.Context) error { return c.Render("page", map[string]any{"input": c.Query("input")}) })
	resp, err := adapter.WrappedRouter().Test(httptest.NewRequest("GET", "/page?input=%3Cscript%3Ebad%3C/script%3E", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 500 || strings.Contains(string(body), "<script>") {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
}

type contractTag string

func (n contractTag) Execute(_ *p7.ExecutionContext, w p7.TemplateWriter) error {
	_, err := w.WriteString(string(n))
	return err
}

func TestFiberV3ViewScopedExtensions(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(fmt.Sprint(reload), func(t *testing.T) {
			var views [2]router.Views
			for i := range views {
				views[i] = newContractViews(t, `{{ input|custom }}{% stamp %}`, nil, reload)
				if err := views[i].Load(); err == nil {
					t.Fatal("unregistered extension unexpectedly loaded")
				}
				ext := views[i].(router.FiberV3ViewExtensions)
				prefix := fmt.Sprint(i)
				fn := func(in, param *p7.Value) (*p7.Value, error) { return p7.AsValue(prefix + in.String()), nil }
				parser := func(_ *p7.Parser, _ *p7.Token, _ *p7.Parser) (p7.INodeTag, error) { return contractTag(prefix), nil }
				if err := ext.RegisterFilter("custom", fn); err != nil {
					t.Fatal(err)
				}
				if err := ext.RegisterTag("stamp", parser); err != nil {
					t.Fatal(err)
				}
				for _, err := range []error{ext.RegisterFilter("custom", fn), ext.RegisterFilter("escape", fn), ext.RegisterFilter("", fn), ext.RegisterFilter("nil", nil), ext.RegisterTag("stamp", parser), ext.RegisterTag("if", parser), ext.RegisterTag("", parser), ext.RegisterTag("nil", nil)} {
					if err == nil {
						t.Fatal("invalid registration accepted")
					}
				}
			}
			var workers sync.WaitGroup
			for i := range 8 {
				workers.Go(func() {
					for range 5 {
						var out bytes.Buffer
						if err := views[i%2].Render(&out, "page", map[string]any{"input": "<x>"}); err != nil {
							t.Error(err)
							return
						}
						want := fmt.Sprintf("%d&lt;x&gt;%d", i%2, i%2)
						if out.String() != want {
							t.Errorf("got %q want %q", out.String(), want)
						}
					}
				})
			}
			workers.Wait()
		})
	}
}

type versionedViewConfig struct {
	*router.SimpleViewConfig
	modern map[string]any
}

func (c *versionedViewConfig) GetFiberV3TemplateFunctions() map[string]any { return c.modern }

func TestFiberV3ViewFunctionOverrides(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		for _, reload := range []bool{false, true} {
			t.Run(fmt.Sprintf("embed=%t/reload=%t", embedded, reload), func(t *testing.T) {
				dir := t.TempDir()
				source := []byte(`{{ echo(input) }}|{% if allowed() %}bad{% else %}false{% endif %}|{{ trusted() }}`)
				if err := os.WriteFile(filepath.Join(dir, "page.html"), source, 0600); err != nil {
					t.Fatal(err)
				}
				cfg := &versionedViewConfig{SimpleViewConfig: router.NewSimpleViewConfig(dir).WithDebug(false).WithReload(reload), modern: map[string]any{
					"echo":    func(v *p7.Value) *p7.Value { return v },
					"allowed": func() *p7.Value { return p7.AsValue(false) },
					"trusted": func() *p7.Value { return p7.AsSafeValue("<b>trusted</b>") },
				}}
				cfg.WithFunctions(map[string]any{
					"echo":    func(v *p6.Value) *p6.Value { return v },
					"allowed": func() *p6.Value { return p6.AsValue(false) },
					"trusted": func() *p6.Value { return p6.AsSafeValue("<b>trusted</b>") },
				})
				if embedded {
					cfg.WithEmbedFS(".", fstest.MapFS{"page.html": {Data: source}})
				}
				legacy, err := router.InitializeViewEngine(cfg)
				if err != nil {
					t.Fatal(err)
				}
				modern, err := router.InitializeFiberV3ViewEngine(cfg)
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					for _, views := range []router.Views{legacy, modern} {
						var out bytes.Buffer
						if err := views.Render(&out, "page", map[string]any{"input": "<x>"}); err != nil {
							t.Fatal(err)
						}
						if want := `&lt;x&gt;|false|<b>trusted</b>`; out.String() != want {
							t.Fatalf("got %q want %q", out.String(), want)
						}
					}
				}
				// Constructors must not replace the shared v6 function map.
				if _, ok := cfg.Functions["echo"].(func(*p6.Value) *p6.Value); !ok {
					t.Fatal("shared functions changed")
				}
			})
		}
	}
}
