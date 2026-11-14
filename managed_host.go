package router

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ManagedServer is an optional native Fiber capability. Server remains unchanged.
// A successful StartManaged reserves a single-use lifetime, then initializes and
// binds asynchronously. Configure routes before calling it. Do not call raw
// native lifecycle/mutation APIs, Test, or Server setters during that lifetime.
type ManagedServer interface {
	StartManaged(context.Context, ManagedOptions) (ManagedRun, error)
}

// ManagedRun distinguishes binding, admission readiness, stopping and final
// completion. Bound/Serving close only when reached; select them with Done.
// Stop latches shutdown even if its caller context is already canceled. Its
// context bounds only that caller's wait. A shared drain deadline may return
// ErrManagedDrainTimeout before Done if application code has not cooperated.
// Wait reports the final sticky result after all owned workers have joined.
type ManagedRun interface {
	Bound() <-chan struct{}
	Serving() <-chan struct{}
	Stopping() <-chan struct{}
	Done() <-chan struct{}
	Address() string
	State() ManagedState
	Stop(context.Context) error
	Wait(context.Context) error
}

type ManagedState string

const (
	ManagedCreated      ManagedState = "created"
	ManagedInitializing ManagedState = "initializing"
	ManagedBound        ManagedState = "bound"
	ManagedServing      ManagedState = "serving"
	ManagedStopping     ManagedState = "stopping"
	ManagedStopped      ManagedState = "stopped"
)

// ManagedOptions selects one owned plain TCP listener (tcp, tcp4 or tcp6).
// TLS/autocert, prefork and Unix sockets are not supported in managed mode.
// The native Serve path retains its existing listen configuration.
//
// OnBound runs after initialization and binding but before native OnListen and
// serving. It receives the startup context and actual address; returning an error
// prevents serving. Native OnListen/BeforeServe callbacks still run, but their
// panics are classified separately from returned errors.
//
// OnShutdown runs once when stopping starts, concurrently with any pending
// startup callback and HTTP drain. Its independent deadline is DrainTimeout.
// Use it to close/join application-owned SSE/WebSocket resources. Native shutdown
// hooks are not invoked by managed hosting. Callbacks must not wait for this
// run's Done/Stop/Wait; callers may instead observe Stopping externally.
//
// Parent cancellation requests stop. Its cause is retained if observable when
// stop first latches; later cancellation does not rewrite an explicit stop.
// It does not cancel admitted request contexts until drain expiry. Requests
// inherit parent values; middleware must derive replacements from the current
// context to retain managed cancellation. Ordinary request contexts expire on handler return; response-stream contexts
// remain valid until the connection is idle/closed or the drain deadline expires.
// Explicit Stop with successful drain returns nil. Deadline errors match both
// ErrManagedDrainTimeout and context.DeadlineExceeded. Panics/errors can be joined.
type ManagedOptions struct {
	Address      string
	Network      string
	DrainTimeout time.Duration
	OnBound      func(context.Context, string) error
	OnShutdown   func(context.Context) error
}

var (
	ErrManagedOptions            = errors.New("invalid managed hosting options")
	ErrManagedAlreadyStarted     = errors.New("managed lifetime already started")
	ErrHostingMode               = errors.New("incompatible hosting mode or stopped lifetime")
	ErrManagedDrainTimeout       = errors.New("managed drain deadline exceeded")
	ErrManagedNativeRegistration = errors.New("unaccounted native registration")
	ErrManagedUnsupported        = errors.New("unsupported managed native configuration")
)

// ManagedError identifies the failing phase and preserves its cause. This is a
// host diagnostic; it is not a public HTTP error envelope.
type ManagedError struct {
	Phase string
	Err   error
}

func (e *ManagedError) Error() string { return fmt.Sprintf("managed host %s: %v", e.Phase, e.Err) }
func (e *ManagedError) Unwrap() error { return e.Err }

// ManagedPanicError distinguishes a panic from a returned callback error. Value
// is privileged diagnostic data. An error-valued panic remains inspectable.
type ManagedPanicError struct{ Value any }

func (e *ManagedPanicError) Error() string { return "managed host callback/native panic" }
func (e *ManagedPanicError) Unwrap() error { err, _ := e.Value.(error); return err }

func (o ManagedOptions) validated() (ManagedOptions, error) {
	if o.Address == "" || o.DrainTimeout <= 0 {
		return o, &ManagedError{"options", ErrManagedOptions}
	}
	if o.Network == "" {
		o.Network = "tcp"
	}
	switch o.Network {
	case "tcp", "tcp4", "tcp6":
	default:
		return o, &ManagedError{"network", ErrManagedUnsupported}
	}
	return o, nil
}
