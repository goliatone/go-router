package router

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagedFlushConfiguration(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := managedFixtureFor(t, major, FiberAdapterConfig{})
			f.get("/ok", func(c Context) error { return c.SendString("ok") })
			f.onListen(func() error {
				switch a := f.server.(type) {
				case *FiberAdapter:
					a.app.Server().ReduceMemoryUsage = false
				case *FiberV3Adapter:
					a.app.Server().ReduceMemoryUsage = false
				}
				return nil
			})
			run := managedStart(t, f, context.Background(), managedOptions())
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := run.Wait(ctx); !errors.Is(err, ErrManagedUnsupported) {
				t.Fatalf("disabled flush mode: %v", err)
			}
			select {
			case <-run.Serving():
				t.Fatal("served with disabled flush protection")
			default:
			}
		})
	}
}

func TestManagedFlushLeavesNativeSetupUnchanged(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := managedFixtureFor(t, major, FiberAdapterConfig{})
			f.get("/ok", func(c Context) error { return c.SendString("ok") })
			if err := f.initialize(); err != nil {
				t.Fatal(err)
			}
			f.raw()
			switch a := f.server.(type) {
			case *FiberAdapter:
				if a.app.Server().ReduceMemoryUsage {
					t.Fatal("native setup changed flush policy")
				}
			case *FiberV3Adapter:
				if a.app.Server().ReduceMemoryUsage {
					t.Fatal("native setup changed flush policy")
				}
			}
		})
	}
}
