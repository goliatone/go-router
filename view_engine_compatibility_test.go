package router_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	pongo2 "github.com/flosch/pongo2/v6"
	p7 "github.com/flosch/pongo2/v7"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

var registerLegacyViewExtensions = sync.OnceValue(func() error {
	if err := pongo2.RegisterFilter("router_compat_filter", func(in, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
		return pongo2.AsValue(param.String() + in.String()), nil
	}); err != nil {
		return err
	}
	return pongo2.RegisterTag("router_compat_tag", func(doc *pongo2.Parser, start *pongo2.Token, args *pongo2.Parser) (pongo2.INodeTag, *pongo2.Error) {
		expr, err := args.ParseExpression()
		if err != nil {
			return nil, err
		}
		if args.Remaining() != 0 {
			return nil, args.Error("unexpected arguments", start)
		}
		body, _, err := doc.WrapUntilTag("endrouter_compat_tag")
		if err != nil {
			return nil, err
		}
		return &legacyViewTag{expr: expr, body: body}, nil
	})
})

type legacyViewTag struct {
	expr pongo2.IEvaluator
	body *pongo2.NodeWrapper
}

func (n *legacyViewTag) Execute(ctx *pongo2.ExecutionContext, out pongo2.TemplateWriter) *pongo2.Error {
	v, err := n.expr.Evaluate(ctx)
	if err != nil {
		return err
	}
	child := pongo2.NewChildExecutionContext(ctx)
	child.Private["custom"] = v
	return n.body.Execute(child, out)
}

// The v7 port keeps the tag's template syntax and child-context behavior.
type modernViewTag struct {
	expr p7.IEvaluator
	body *p7.NodeWrapper
}

func (n *modernViewTag) Execute(ctx *p7.ExecutionContext, out p7.TemplateWriter) error {
	v, err := n.expr.Evaluate(ctx)
	if err != nil {
		return err
	}
	child := p7.NewChildExecutionContext(ctx)
	child.Private["custom"] = v
	return n.body.Execute(child, out)
}

func configureModernViewExtensions(extensions router.FiberV3ViewExtensions) error {
	if err := extensions.RegisterFilter("router_compat_filter", func(in, param *p7.Value) (*p7.Value, error) {
		return p7.AsValue(param.String() + in.String()), nil
	}); err != nil {
		return err
	}
	return extensions.RegisterTag("router_compat_tag", func(doc *p7.Parser, start *p7.Token, args *p7.Parser) (p7.INodeTag, error) {
		expr, err := args.ParseExpression()
		if err != nil {
			return nil, err
		}
		if args.Remaining() != 0 {
			return nil, args.Error("unexpected arguments", start)
		}
		body, _, err := doc.WrapUntilTag("endrouter_compat_tag")
		if err != nil {
			return nil, err
		}
		return &modernViewTag{expr: expr, body: body}, nil
	})
}

func TestFiberV3LegacyViewExtensions(t *testing.T) {
	if err := registerLegacyViewExtensions(); err != nil {
		t.Fatal(err)
	}
	for _, reload := range []bool{false, true} {
		for _, embedded := range []bool{false, true} {
			t.Run(fmt.Sprintf("reload=%t/embed=%t", reload, embedded), func(t *testing.T) {
				dir := t.TempDir()
				sources := fstest.MapFS{}
				for name, source := range map[string]string{
					"base.html":   `{% block content %}{% endblock %}`,
					"page.html":   `{% extends "base.html" %}{% block content %}{% router_compat_tag input %}{{ custom|router_compat_filter:"prefix:" }}{% endrouter_compat_tag %}/{% include "inc.html" %}{% endblock %}`,
					"inc.html":    `{{ typed(input) }}/{{ safe(input) }}/{% if allowed() %}allowed{% else %}denied{% endif %}`,
					"layout.html": `[{{ embed|safe }}]`,
				} {
					sources[name] = &fstest.MapFile{Data: []byte(source)}
					if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				cfg := router.NewSimpleViewConfig(dir).WithDebug(false).WithReload(reload).WithFunctions(map[string]any{
					"typed":   func(v *pongo2.Value) *pongo2.Value { return v },
					"safe":    func(s string) *pongo2.Value { return pongo2.AsSafeValue(s) },
					"allowed": func() *pongo2.Value { return pongo2.AsValue(false) },
				}).WithFiberV3Functions(map[string]any{
					"typed":   func(v *p7.Value) *p7.Value { return v },
					"safe":    func(s string) *p7.Value { return p7.AsSafeValue(s) },
					"allowed": func() *p7.Value { return p7.AsValue(false) },
				}).WithFiberV3Extensions(configureModernViewExtensions)
				if embedded {
					cfg.WithEmbedFS(".", sources)
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
					for name, views := range map[string]router.Views{"legacy": legacy, "v3": modern} {
						var out bytes.Buffer
						if err := views.Render(&out, "page", map[string]any{"input": "<b>x</b>"}, "layout"); err != nil {
							t.Errorf("%s: %v", name, err)
							continue
						}
						if want := "[prefix:&lt;b&gt;x&lt;/b&gt;/&lt;b&gt;x&lt;/b&gt;/<b>x</b>/denied]"; out.String() != want {
							t.Errorf("%s: got %q; want %q", name, out.String(), want)
						}
					}
				}
				// Exercise the production router path with the same configured ports.
				server := router.NewFiberV3Adapter(func(*fiber3.App) *fiber3.App {
					return fiber3.New(fiber3.Config{Views: modern, PassLocalsToViews: true})
				})
				server.Router().Get("/page", func(c router.Context) error {
					c.Locals("input", "wrong local")
					return c.Render("page", map[string]any{"input": "<b>x</b>"}, "layout")
				})
				code, body, headers := v3Response(t, server.WrappedRouter(), "GET", "/page", "")
				if want := "[prefix:&lt;b&gt;x&lt;/b&gt;/&lt;b&gt;x&lt;/b&gt;/<b>x</b>/denied]"; code != 200 || body != want || headers.Get("Content-Type") != "text/html; charset=utf-8" {
					t.Fatalf("configured HTTP view: status=%d body=%q headers=%v", code, body, headers)
				}
			})
		}
	}
}

func TestFiberV3ViewExtensionConfigurationErrors(t *testing.T) {
	failure := errors.New("extension configuration failed")
	calls := 0
	cfg := router.NewSimpleViewConfig(t.TempDir()).WithDebug(false).
		WithFiberV3Extensions(func(router.FiberV3ViewExtensions) error {
			calls++
			return failure
		})
	// The same config remains usable by v2; its constructor must ignore v3 ports.
	if _, err := router.InitializeViewEngine(cfg); err != nil || calls != 0 {
		t.Fatalf("v2 used v3 configuration: calls=%d err=%v", calls, err)
	}
	if views, err := router.DefaultFiberV3ViewEngine(cfg); views != nil || !errors.Is(err, failure) {
		t.Fatalf("default constructor lost configuration error: views=%v err=%v", views, err)
	}
	if views, err := router.InitializeFiberV3ViewEngine(cfg); views != nil || !errors.Is(err, failure) {
		t.Fatalf("initialized constructor lost configuration error: views=%v err=%v", views, err)
	}
	if calls != 2 {
		t.Fatalf("expected one configuration call per v3 constructor: %d", calls)
	}
}

func TestFiberV3LegacyHelperValues(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		legacy, modern     any
	}{
		{"unsafe", `{{ helper(input) }}`, "&lt;script&gt;alert(1)&lt;/script&gt;", func(s string) *pongo2.Value { return pongo2.AsValue(s) }, func(s string) *p7.Value { return p7.AsValue(s) }},
		{"safe", `{{ helper(input) }}`, "<script>alert(1)</script>", func(s string) *pongo2.Value { return pongo2.AsSafeValue(s) }, func(s string) *p7.Value { return p7.AsSafeValue(s) }},
		{"false", `{% if helper(input) %}allowed{% else %}denied{% endif %}`, "denied", func(string) *pongo2.Value { return pongo2.AsValue(false) }, func(string) *p7.Value { return p7.AsValue(false) }},
		{"typed", `{{ helper(input) }}`, "&lt;script&gt;alert(1)&lt;/script&gt;", func(v *pongo2.Value) *pongo2.Value { return v }, func(v *p7.Value) *p7.Value { return v }},
		{"interface", `{{ helper(input) }}`, "&lt;script&gt;alert(1)&lt;/script&gt;", func(s string) any { return pongo2.AsValue(s) }, func(s string) any { return p7.AsValue(s) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := router.NewSimpleViewConfig(dir).WithDebug(false).WithReload(false).
				WithFunctions(map[string]any{"helper": tc.legacy}).
				WithFiberV3Functions(map[string]any{"helper": tc.modern})
			views, err := router.DefaultFiberV3ViewEngine(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := views.Render(&out, "page", map[string]any{"input": "<script>alert(1)</script>"}); err != nil || out.String() != tc.want {
				t.Fatalf("got %q, %v; want %q", out.String(), err, tc.want)
			}
		})
	}
}
