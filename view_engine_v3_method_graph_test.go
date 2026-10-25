package router_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	p6 "github.com/flosch/pongo2/v6"
	p7 "github.com/flosch/pongo2/v7"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
)

type graphLeaf struct{ Input string }

func (l graphLeaf) Payload() any { return p6.AsValue(l.Input) }
func (l graphLeaf) Allowed() any { return p6.AsValue(false) }

type graphModel struct{ Input string }

func (m graphModel) Leaf() graphLeaf { return graphLeaf{m.Input} }

type graphPointerLeaf struct{ Input string }

func (l *graphPointerLeaf) Payload() *p6.Value { return p6.AsValue(l.Input) }

type graphPointerModel struct{ Input string }

func (m graphPointerModel) Pointer() *graphPointerLeaf { return &graphPointerLeaf{m.Input} }

type graphSliceModel struct{ Input string }

func (m graphSliceModel) Items() []*graphPointerLeaf { return []*graphPointerLeaf{{m.Input}} }

type graphMapModel struct{ Input string }

func (m graphMapModel) Items() map[string]*graphPointerLeaf {
	return map[string]*graphPointerLeaf{"item": {m.Input}}
}

type graphEnvelope struct{ Item *graphPointerLeaf }
type graphFieldModel struct{ Input string }

func (m graphFieldModel) Envelope() graphEnvelope { return graphEnvelope{&graphPointerLeaf{m.Input}} }

type graphError struct{ input string }

func (graphError) Error() string        { return "problem" }
func (e graphError) Payload() *p6.Value { return p6.AsValue(e.input) }
func (graphError) Allowed() *p6.Value   { return p6.AsValue(false) }

type graphErrorModel struct{ Input string }

func (m graphErrorModel) Problem() error { return graphError{m.Input} }

type graphInterfaceModel struct{ Input string }

func (m graphInterfaceModel) Leaf() any { return graphLeaf{m.Input} }

type graphConcreteErrorModel struct{ Input string }

func (m graphConcreteErrorModel) Problem() graphError { return graphError{m.Input} }

type graphCycleStart struct{}

func (graphCycleStart) Start() graphCycleA { return graphCycleA{} }

type graphCycleA struct{}

func (graphCycleA) Next() graphCycleB { return graphCycleB{} }

type graphCycleB struct{}

func (graphCycleB) Back() graphCycleA { return graphCycleA{} }
func (graphCycleB) Payload() any      { return p6.AsValue("<script>cycle</script>") }

type graphClosed struct{ Title string }

func (m graphClosed) Next() graphClosed                  { return m }
func (m graphClosed) Text() string                       { return m.Title }
func (m graphClosed) Result() (string, error)            { return m.Title, nil }
func (m graphClosed) Failure() (string, error)           { return "", errors.New("method failure") }
func (m graphClosed) TypedResult() (string, *graphError) { return m.Title, &graphError{} }

type graphSafeError struct{ Message string }

func (e graphSafeError) Error() string { return e.Message }

type graphSafeErrorModel struct{}

func (graphSafeErrorModel) Problem() graphSafeError { return graphSafeError{"<safe-error>"} }

type graphValueReceiver struct{ Text string }

func (m graphValueReceiver) Next() graphValueReceiver { return m }
func (*graphValueReceiver) Dangerous() any            { return p6.AsValue("<script>pointer</script>") }

func TestFiberV3ViewTransitiveMethodBoundary(t *testing.T) {
	const input = "<script>bad</script>"
	for _, embedded := range []bool{false, true} {
		for _, reload := range []bool{false, true} {
			t.Run(fmt.Sprintf("embed=%t/reload=%t", embedded, reload), func(t *testing.T) {
				for _, tc := range []struct {
					name, source string
					model        any
				}{
					{"concrete", `{{ model.Leaf().Payload() }}`, graphModel{input}},
					{"pointer", `{{ model.Pointer().Payload() }}`, graphPointerModel{input}},
					{"slice", `{{ model.Items().0.Payload() }}`, graphSliceModel{input}},
					{"map", `{{ model.Items().item.Payload() }}`, graphMapModel{input}},
					{"field", `{{ model.Envelope().Item.Payload() }}`, graphFieldModel{input}},
					{"interface", `{{ model.Leaf().Payload() }}`, graphInterfaceModel{input}},
					{"error-interface", `{{ model.Problem().Payload() }}`, graphErrorModel{input}},
					{"concrete-error", `{{ model.Problem().Payload() }}`, graphConcreteErrorModel{input}},
					{"false", `{% if model.Leaf().Allowed() %}allowed{% else %}denied{% endif %}`, graphModel{input}},
					{"error-false", `{% if model.Problem().Allowed() %}allowed{% else %}denied{% endif %}`, graphErrorModel{input}},
					{"recursive-types", `{{ model.Start().Next().Payload() }}`, graphCycleStart{}},
				} {
					t.Run(tc.name, func(t *testing.T) {
						views := graphViews(t, tc.source, embedded, reload)
						for range 2 {
							var out bytes.Buffer
							err := views.Render(&out, "page", map[string]any{"model": tc.model})
							if err == nil || !strings.Contains(err.Error(), "migrate helpers/data") || strings.Contains(out.String(), "<script>") || out.String() == "allowed" {
								t.Fatalf("unsafe graph: output=%q err=%v", out.String(), err)
							}
						}
					})
				}
			})
		}
	}
	// Results from both registered helpers and filters pass through the graph proof.
	for _, source := range []string{`{{ model(input).Leaf().Payload() }}`, `{% with input|model_port as model %}{{ model.Leaf().Payload() }}{% endwith %}`, `{{ problem(input).Problem().Payload() }}`, `{% with input|error_port as model %}{{ model.Problem().Payload() }}{% endwith %}`} {
		views := newContractViews(t, source, map[string]any{"model": func(s string) graphModel { return graphModel{s} }, "problem": func(s string) graphErrorModel { return graphErrorModel{s} }}, false)
		ext := views.(router.FiberV3ViewExtensions)
		if err := ext.RegisterFilter("model_port", func(in, _ *p7.Value) (*p7.Value, error) { return p7.AsValue(graphModel{in.String()}), nil }); err != nil {
			t.Fatal(err)
		}
		if err := ext.RegisterFilter("error_port", func(in, _ *p7.Value) (*p7.Value, error) { return p7.AsValue(graphErrorModel{in.String()}), nil }); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err := views.Render(&out, "page", map[string]any{"input": input})
		if err == nil || !strings.Contains(err.Error(), "migrate helpers/data") || strings.Contains(out.String(), "<script>") {
			t.Fatalf("helper/filter graph escaped: %q %v", out.String(), err)
		}
	}
	views := newContractViews(t, `{{ model(input).Leaf().Payload() }}`, map[string]any{"model": func(s string) graphModel { return graphModel{s} }}, false)
	server := router.NewFiberV3Adapter(func(*fiber3.App) *fiber3.App { return fiber3.New(fiber3.Config{Views: views}) })
	server.Router().Get("/page", func(c router.Context) error { return c.Render("page", map[string]any{"input": c.Query("input")}) })
	code, body, _ := v3Response(t, server.WrappedRouter(), "GET", "/page?input=%3Cscript%3Ebad%3C/script%3E", "")
	if code != 500 || strings.Contains(body, "<script>") {
		t.Fatalf("HTTP boundary: %d %q", code, body)
	}
}

func TestFiberV3ViewClosedMethodGraph(t *testing.T) {
	views := newContractViews(t, `{{ model.Next().Next().Text() }}|{{ model.Result() }}|{{ stamp.Format("2006") }}`, nil, false)
	for range 2 {
		var out bytes.Buffer
		if err := views.Render(&out, "page", map[string]any{"model": graphClosed{"<x>"}, "stamp": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
		if want := `&lt;x&gt;|&lt;x&gt;|2026`; out.String() != want {
			t.Fatalf("got %q want %q", out.String(), want)
		}
	}
	// A method's second error result is handled by the evaluator rather than
	// exposed as data, even if its concrete error type has extra methods.
	for _, tc := range []struct{ source, want string }{
		{`{{ model.Failure() }}`, "method failure"},
		{`{{ model.TypedResult() }}`, "problem"},
	} {
		views = newContractViews(t, tc.source, nil, false)
		if err := views.Render(io.Discard, "page", map[string]any{"model": graphClosed{}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("error result lost: %v", err)
		}
	}
	views = newContractViews(t, `{{ model.Problem().Error() }}|{{ receiver.Next().Text }}`, nil, false)
	var out bytes.Buffer
	if err := views.Render(&out, "page", map[string]any{"model": graphSafeErrorModel{}, "receiver": graphValueReceiver{Text: "<value>"}}); err != nil || out.String() != `&lt;safe-error&gt;|&lt;value&gt;` {
		t.Fatalf("closed error/value receiver changed: %q %v", out.String(), err)
	}

}

func graphViews(t *testing.T, source string, embedded, reload bool) router.Views {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := router.NewSimpleViewConfig(dir).WithDebug(false).WithReload(reload)
	if embedded {
		cfg.WithEmbedFS(".", fstest.MapFS{"page.html": {Data: []byte(source)}})
	}
	views, err := router.DefaultFiberV3ViewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return views
}
