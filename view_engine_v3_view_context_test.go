package router_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	p6 "github.com/flosch/pongo2/v6"
	p7 "github.com/flosch/pongo2/v7"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
	"github.com/goliatone/go-router/flash"
	flashmiddleware "github.com/goliatone/go-router/middleware/flash"
)

type viewContextHelper func() (*p7.Value, error)

func TestFiberV3ViewNamedHelperContainers(t *testing.T) {
	helper := viewContextHelper(func() (*p7.Value, error) { return p7.AsValue("<helper>"), nil })
	for _, tc := range []struct {
		source string
		data   any
	}{
		{`{{ helpers.hello() }}`, map[string]viewContextHelper{"hello": helper}},
		{`{{ helpers.0() }}`, []viewContextHelper{helper}},
	} {
		views := newContractViews(t, tc.source, nil, false)
		var out bytes.Buffer
		if err := views.Render(&out, "page", map[string]any{"helpers": tc.data}); err != nil || out.String() != "&lt;helper&gt;" {
			t.Fatalf("safe named helper rejected: %q %v", out.String(), err)
		}
	}
}

func TestFiberV3ViewContextData(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(fmt.Sprintf("reload=%t", reload), func(t *testing.T) {
			data := router.ViewContext{
				"message": "<hello>", "allowed": false,
				"nested": router.ViewContext{"message": "<nested>"},
				"safe":   p7.AsSafeValue("<b>safe</b>"),
				"helper": func() router.ViewContext { return router.ViewContext{"message": "<helper>"} },
			}
			views := newContractViews(t, `{{ flash.message }}|{% if flash.allowed %}bad{% else %}false{% endif %}|{{ flash.nested.message }}|{{ flash.safe }}|{{ flash.helper().message }}|{{ rows.0.message }}`, nil, reload)
			for range 2 {
				var out bytes.Buffer
				if err := views.Render(&out, "page", router.ViewContext{"flash": data, "rows": []any{data}}); err != nil {
					t.Fatal(err)
				}
				if want := `&lt;hello&gt;|false|&lt;nested&gt;|<b>safe</b>|&lt;helper&gt;|&lt;hello&gt;`; out.String() != want {
					t.Fatalf("got %q want %q", out.String(), want)
				}
			}
			// Templates receive copied plain maps, including nested contexts. A
			// helper can inspect/mutate its input without touching caller data.
			views = newContractViews(t, `{{ inspect(flash) }}`, map[string]any{
				"inspect": func(v *p7.Value) (string, error) {
					m, ok := v.Interface().(map[string]any)
					if !ok {
						return "", fmt.Errorf("context retained methods: %T", v.Interface())
					}
					nested, ok := m["nested"].(map[string]any)
					if !ok {
						return "", fmt.Errorf("nested context retained methods: %T", m["nested"])
					}
					m["message"], nested["message"] = "changed", "changed"
					return "copied", nil
				},
			}, reload)
			if err := views.Render(io.Discard, "page", map[string]any{"flash": data}); err != nil {
				t.Fatal(err)
			}
			if data["message"] != "<hello>" || data["nested"].(router.ViewContext)["message"] != "<nested>" {
				t.Fatalf("caller data mutated: %#v", data)
			}
		})
	}
	views := newContractViews(t, `{% if flash %}bad{% else %}empty{% endif %}`, nil, false)
	for _, data := range []router.ViewContext{nil, {}} {
		var out bytes.Buffer
		if err := views.Render(&out, "page", map[string]any{"flash": data}); err != nil || out.String() != "empty" {
			t.Fatalf("empty context: %q %v", out.String(), err)
		}
	}
}

func TestFiberV3ViewContextSafetyBoundary(t *testing.T) {
	plain := router.ViewContext{"message": "hello"}
	cycle := router.ViewContext{}
	cycle["cycle"] = cycle
	for _, tc := range []struct {
		name string
		data any
	}{
		{"foreign-value", router.ViewContext{"value": p6.AsValue("<script>bad</script>")}},
		{"foreign-safe", router.ViewContext{"value": p6.AsSafeValue("<script>bad</script>")}},
		{"foreign-false", router.ViewContext{"value": p6.AsValue(false)}},
		{"foreign-signature", router.ViewContext{"value": func() *p6.Value { return p6.AsValue(false) }}},
		{"dynamic-model", router.ViewContext{"value": foreignDynamicModel{}}},
		{"dynamic-map", router.ViewContext{"value": foreignDynamicMap{}}},
		{"transitive-model", router.ViewContext{"value": graphModel{"<script>bad</script>"}}},
		{"boxed-foreign", router.ViewContext{"value": p7.AsValue(p6.AsValue(false))}},
		{"cycle", cycle},
		// These containers retain the original method surface. They must not
		// reattach ViewContext.Update after validating a temporary plain map.
		{"boxed-context", p7.AsValue(plain)},
		{"pointer-context", &plain},
		{"field-context", struct{ Flash router.ViewContext }{plain}},
		{"typed-map", map[string]router.ViewContext{"flash": plain}},
		{"typed-slice", []router.ViewContext{plain}},
		{"typed-array", [1]router.ViewContext{plain}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			views := newContractViews(t, `{{ flash }}`, nil, false)
			var out bytes.Buffer
			err := views.Render(&out, "page", map[string]any{"flash": tc.data})
			if err == nil || !strings.Contains(err.Error(), "migrate helpers/data") || out.Len() != 0 {
				t.Fatalf("unsafe data accepted: output=%q err=%v", out.String(), err)
			}
		})
	}
	views := newContractViews(t, `{{ flash.helper().value }}`, nil, false)
	if err := views.Render(io.Discard, "page", map[string]any{"flash": router.ViewContext{
		"helper": func() router.ViewContext { return router.ViewContext{"value": p6.AsValue(false)} },
	}}); err == nil || !strings.Contains(err.Error(), "pongo2/v6.Value") {
		t.Fatalf("dynamic helper result escaped boundary: %v", err)
	}
}

func TestFiberV3FlashMiddlewareViewContext(t *testing.T) {
	views := newContractViews(t, `{% if flash.message %}{{ flash.message }}{% else %}empty{% endif %}`, nil, false)
	server := router.NewFiberV3Adapter(func(*fiber3.App) *fiber3.App {
		return fiber3.New(fiber3.Config{Views: views, PassLocalsToViews: true})
	})
	messages := flash.New(flash.Config{Name: "view-context-flash"})
	server.Router().Use(flashmiddleware.New(flashmiddleware.Config{Flash: messages}))
	server.Router().Get("/page", func(c router.Context) error { return c.Render("page", nil) })
	server.Router().Get("/set", func(c router.Context) error {
		return messages.Redirect(c, "/page", router.ViewContext{"message": "<hello>"})
	})
	request := func(path string, cookie *http.Cookie) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), "GET", path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := server.WrappedRouter().Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil {
			t.Fatal(err)
		}
		return resp, string(body)
	}
	if resp, body := request("/page", nil); resp.StatusCode != 200 || body != "empty" {
		t.Fatalf("empty flash: %d %q", resp.StatusCode, body)
	}
	resp, _ := request("/set", nil)
	if resp.StatusCode != 302 || resp.Header.Get("Location") != "/page" || len(resp.Cookies()) != 1 {
		t.Fatalf("flash redirect: %d %v", resp.StatusCode, resp.Header)
	}
	resp, body := request("/page", resp.Cookies()[0])
	if resp.StatusCode != 200 || body != "&lt;hello&gt;" {
		t.Fatalf("cookie flash: %d %q", resp.StatusCode, body)
	}
	if cookies := resp.Cookies(); len(cookies) != 1 || cookies[0].Value != "" {
		t.Fatalf("flash was not consumed: %v", cookies)
	}
}
