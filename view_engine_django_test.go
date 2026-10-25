package router_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	router "github.com/goliatone/go-router"
)

type configurableDjangoViews interface {
	router.Views
	SetAutoEscape(bool)
}

func TestDualFiberViewAutoescapeIsolation(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(fmt.Sprintf("reload=%t", reload), func(t *testing.T) {
			dir := t.TempDir()
			for name, source := range map[string]string{
				"base.html":   `B{% block content %}{% endblock %}`,
				"page.html":   `{% extends "base.html" %}{% block content %}{{ value }}/{% include "inc.html" %}{% endblock %}`,
				"inc.html":    `I{{ value }}`,
				"layout.html": `[{{ embed|safe }}]`,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &MockViewConfig{DirOS: dir, Ext: ".html", Reload: reload}
			legacy, err := router.DefaultViewEngine(cfg)
			if err != nil {
				t.Fatal(err)
			}
			modern, err := router.DefaultFiberV3ViewEngine(cfg)
			if err != nil {
				t.Fatal(err)
			}
			old := requireTestType[configurableDjangoViews](t, legacy)
			new := requireTestType[configurableDjangoViews](t, modern)
			render := func(engine configurableDjangoViews, escaped bool) error {
				var out bytes.Buffer
				if err := engine.Render(&out, "page", map[string]any{"value": "<b>unsafe</b>"}, "layout"); err != nil {
					return err
				}
				value := "<b>unsafe</b>"
				if escaped {
					value = "&lt;b&gt;unsafe&lt;/b&gt;"
				}
				if want := "[B" + value + "/I" + value + "]"; out.String() != want {
					return fmt.Errorf("escaped=%t: got %q; want %q", escaped, out.String(), want)
				}
				return nil
			}
			for _, oldEscaped := range []bool{true, false, true} {
				old.SetAutoEscape(oldEscaped)
				new.SetAutoEscape(!oldEscaped)
				// Explicit loading and alternating renders must not leak a flag.
				for _, engine := range []configurableDjangoViews{old, new} {
					if err := engine.Load(); err != nil {
						t.Fatal(err)
					}
				}
				for range 3 {
					if err := render(old, oldEscaped); err != nil {
						t.Fatal(err)
					}
					if err := render(new, !oldEscaped); err != nil {
						t.Fatal(err)
					}
				}
				errors := make(chan error, 8)
				var workers sync.WaitGroup
				for i := range 8 {
					workers.Go(func() {
						engine, escaped := old, oldEscaped
						if i%2 != 0 {
							engine, escaped = new, !oldEscaped
						}
						for range 12 {
							if err := render(engine, escaped); err != nil {
								errors <- err
								return
							}
						}
					})
				}
				workers.Wait()
				close(errors)
				for err := range errors {
					t.Error(err)
				}
			}
			// Configuration changes publish snapshots alongside active renders.
			var workers sync.WaitGroup
			for range 2 {
				workers.Go(func() {
					for i := range 20 {
						new.SetAutoEscape(i%2 == 0)
						if err := new.Render(io.Discard, "page", map[string]any{"value": "<x>"}); err != nil {
							t.Error(err)
						}
					}
				})
			}
			workers.Wait()
		})
	}
}
