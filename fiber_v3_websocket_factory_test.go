package router

import (
	"sync"
	"testing"
)

type t03PatternFactory struct {
	patterns []string
	name     string
}

func (f *t03PatternFactory) AdapterName() string             { return f.name }
func (f *t03PatternFactory) SupportsWebSocket() bool         { return true }
func (f *t03PatternFactory) SupportedContextTypes() []string { return f.patterns }
func (f *t03PatternFactory) CreateWebSocketContext(Context, WebSocketConfig) (WebSocketContext, error) {
	return nil, nil
}

func TestDualFiberWebSocketFactorySelection(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		r := NewWebSocketFactoryRegistry()
		v2 := NewFiberWebSocketFactory(nil)
		v3 := NewFiberV3WebSocketFactory(nil)
		if reverse {
			r.Register("fiber-v3", v3)
			r.Register("fiber", v2)
		} else {
			r.Register("fiber", v2)
			r.Register("fiber-v3", v3)
		}
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				for range 100 {
					r.Register("fiber", v2)
					r.Register("fiber-v3", v3)
					if r.GetByContext(&fiberContext{}) != v2 || r.GetByContext(&fiberV3Context{}) != v3 {
						t.Error("native factory collision")
					}
				}
			})
		}
		wg.Wait()
	}
}

func TestWebSocketFactoryCustomPatternTieIsStable(t *testing.T) {
	r := NewWebSocketFactoryRegistry()
	a := &t03PatternFactory{name: "a", patterns: []string{"MockContext"}}
	b := &t03PatternFactory{name: "b", patterns: []string{"MockContext"}}
	generic := &t03PatternFactory{name: "generic", patterns: []string{"Context"}}
	r.Register("z", b)
	r.Register("a", a)
	r.Register("0", generic)
	for range 100 {
		if r.GetByContext(&MockContext{}) != a {
			t.Fatal("longest pattern/sorted key policy is unstable")
		}
	}
}
