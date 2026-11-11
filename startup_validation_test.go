package router

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestStartupValidation(t *testing.T) {
	t.Run("fiber2", func(t *testing.T) { testStartupValidation(t, NewFiberAdapter()) })
	t.Run("fiber3", func(t *testing.T) { testStartupValidation(t, NewFiberV3Adapter()) })
}
func testStartupValidation[T any](t *testing.T, s Server[T]) {
	rt := s.Router()
	inspector := rt.(RegistrationInspector)
	registrar := rt.Group("/group").(StartupValidatorRegistrar)
	var calls []string
	for _, name := range []string{"z", "a"} {
		if err := registrar.RegisterStartupValidator(name, func() error {
			snap := inspector.RegistrationSnapshot()
			if snap.State != RegistrationSealed {
				return errors.New("snapshot not sealed")
			}
			found := false
			for _, r := range snap.MountedRoutes {
				if r.Path == "/late" {
					found = true
				}
			}
			if !found {
				return errors.New("late route missing")
			}
			calls = append(calls, name)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := registrar.RegisterStartupValidator("a", func() error { return nil }); err == nil {
		t.Fatal("duplicate accepted")
	}
	for _, name := range []string{"", "bad"} {
		if err := registrar.RegisterStartupValidator(name, nil); err == nil {
			t.Fatal("invalid validator accepted")
		}
	}
	before := inspector.RegistrationSnapshot()
	after := inspector.RegistrationSnapshot()
	if before.State != RegistrationCollecting || after.Revision != before.Revision || len(calls) != 0 {
		t.Fatal("read finalized or invoked callbacks")
	}
	rt.Get("/late", func(c Context) error { return c.SendStatus(204) })
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); s.WrappedRouter() }()
	}
	wg.Wait()
	if !reflect.DeepEqual(calls, []string{"a", "z"}) {
		t.Fatalf("calls=%v", calls)
	}
	if err := registrar.RegisterStartupValidator("late", func() error { return nil }); !errors.Is(err, ErrRouterSealed) {
		t.Fatalf("late registration: %v", err)
	}
}

func TestStartupValidationFailure(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run("fiber2", func(t *testing.T) { testStartupFailure(t, NewFiberAdapter(), panics) })
		t.Run("fiber3", func(t *testing.T) { testStartupFailure(t, NewFiberV3Adapter(), panics) })
	}
}
func testStartupFailure[T any](t *testing.T, s Server[T], panics bool) {
	cause := errors.New("invalid exposure")
	calls := 0
	err := s.Router().(StartupValidatorRegistrar).RegisterStartupValidator("exposure", func() error {
		calls++
		if panics {
			panic("broken validator")
		}
		return cause
	})
	if err != nil {
		t.Fatal(err)
	}
	// Initialize must return errors without opening a listener.
	firstInit := s.(StartupInitializer).Initialize()
	// Serve must return before opening a listener, even after prior failures.
	first := s.Serve("invalid-address")
	if first != firstInit {
		t.Fatal("Initialize and Serve disagree")
	}
	var typed *StartupValidationError
	if !errors.As(first, &typed) {
		t.Fatalf("got %T %v", first, first)
	}
	if !panics && !errors.Is(first, cause) {
		t.Fatal("lost cause")
	}
	if panics && !strings.Contains(first.Error(), "validator panicked") {
		t.Fatal(first)
	}
	for range 3 {
		if err := s.Serve("invalid-address"); err != first {
			t.Fatalf("failure changed: %v", err)
		}
		for _, start := range []func(){s.Init, func() { s.WrappedRouter() }} {
			func() {
				defer func() {
					if recovered := recover(); recovered != first {
						t.Errorf("panic=%v want %v", recovered, first)
					}
				}()
				start()
				t.Error("startup accepted failure")
			}()
		}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Serve("invalid-address"); err != first {
				t.Errorf("concurrent retry lost failure: %v", err)
			}
		}()
	}
	wg.Wait()
	if snapshot := s.Router().(RegistrationInspector).RegistrationSnapshot(); snapshot.StartupValidationError != first.Error() {
		t.Fatalf("snapshot lost failure: %+v", snapshot)
	}
	if calls != 1 {
		t.Fatalf("called %d times", calls)
	}
}
