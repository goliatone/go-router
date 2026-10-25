package router_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	legacyCore "github.com/gofiber/template"
	core "github.com/gofiber/template/v2"
	router "github.com/goliatone/go-router"
)

// Isolate a locking regression: a failing child cannot poison the parent's
// engine state or leave blocked test goroutines behind.
func TestFiberViewNestedEngineComposition(t *testing.T) {
	const childKey = "GO_ROUTER_VIEW_COMPOSITION_CHILD"
	if os.Getenv(childKey) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFiberViewNestedEngineComposition$", "-test.timeout=8s")
		cmd.Env = append(os.Environ(), childKey+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("composition subprocess: %v\n%s", err, output)
		}
		return
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"outer.html": `O{{ value }}:{{ fragment()|safe }}:{{ value }}`,
		"inner.html": `I{{ value }}`,
		"held.html":  `{{ hold() }}{{ value }}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, reload := range []bool{false, true} {
		cfg := &MockViewConfig{DirOS: dir, Ext: ".html", Reload: reload}
		create := func(v3 bool) router.Views {
			t.Helper()
			if v3 {
				engine, err := router.DefaultFiberV3ViewEngine(cfg)
				if err != nil {
					t.Fatal(err)
				}
				return engine
			}
			engine, err := router.DefaultViewEngine(cfg)
			if err != nil {
				t.Fatal(err)
			}
			return engine
		}
		for _, outerV3 := range []bool{false, true} {
			for _, innerV3 := range []bool{false, true} {
				outer, inner := create(outerV3), create(innerV3)
				innerValue := "&lt;x&gt;"
				if innerV3 {
					inner.(interface{ SetAutoEscape(bool) }).SetAutoEscape(false)
					innerValue = "<x>"
				}
				addViewFunc(t, outer, "fragment", func() (string, error) {
					var out bytes.Buffer
					err := inner.Render(&out, "inner", map[string]any{"value": "<x>"})
					return out.String(), err
				})
				var out bytes.Buffer
				if err := outer.Render(&out, "outer", map[string]any{"value": "<x>"}); err != nil {
					t.Fatal(err)
				}
				if want := "O&lt;x&gt;:I" + innerValue + ":&lt;x&gt;"; out.String() != want {
					t.Fatalf("legacy/v3 composition %t/%t reload=%t: %q", outerV3, innerV3, reload, out.String())
				}
			}
		}
		// Same-engine nesting and unrelated work/configuration during a held
		// callback must not wait for an execution lock.
		engine := create(true)
		addViewFunc(t, engine, "fragment", func() (string, error) {
			var out bytes.Buffer
			err := engine.Render(&out, "inner", map[string]any{"value": "<x>"})
			return out.String(), err
		})
		if err := engine.Render(&bytes.Buffer{}, "outer", map[string]any{"value": "<x>"}); err != nil {
			t.Fatal(err)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		addViewFunc(t, engine, "hold", func() string { close(entered); <-release; return "" })
		if err := engine.Load(); err != nil {
			t.Fatal(err)
		}
		var held bytes.Buffer
		done := make(chan error, 1)
		go func() { done <- engine.Render(&held, "held", map[string]any{"value": "<x>"}) }()
		<-entered
		engine.(interface{ SetAutoEscape(bool) }).SetAutoEscape(false)
		var other bytes.Buffer
		if err := engine.Render(&other, "inner", map[string]any{"value": "<x>"}); err != nil {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; err != nil || held.String() != "&lt;x&gt;" || other.String() != "I<x>" {
			t.Fatalf("snapshot isolation: held=%q next=%q err=%v", held.String(), other.String(), err)
		}
	}
}

func addViewFunc(t *testing.T, engine router.Views, name string, fn any) {
	t.Helper()
	switch engine := engine.(type) {
	case interface {
		AddFunc(string, any) core.IEngineCore
	}:
		engine.AddFunc(name, fn)
	case interface {
		AddFunc(string, any) legacyCore.IEngineCore
	}:
		engine.AddFunc(name, fn)
	default:
		t.Fatal("template core configuration missing")
	}
}

func TestFiberV3ViewCachedExecutionState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.html"), []byte(`{% for x in values %}{% cycle "a" "b" %}{% ifchanged x %}{{ x }}{% endifchanged %}{% endfor %}`), 0600); err != nil {
		t.Fatal(err)
	}
	engine, err := router.DefaultFiberV3ViewEngine(&MockViewConfig{DirOS: dir, Ext: ".html"})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Load(); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 12 {
				var out bytes.Buffer
				if err := engine.Render(&out, "state", map[string]any{"values": []int{1, 1, 2}}); err != nil || out.String() != "a1ba2" {
					t.Error(fmt.Errorf("per-render tag state: %q %v", out.String(), err))
					return
				}
			}
		})
	}
	workers.Wait()
}
