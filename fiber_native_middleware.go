package router

import (
	"errors"
	"slices"
	"sync/atomic"

	f2 "github.com/gofiber/fiber/v2"
	f3 "github.com/gofiber/fiber/v3"
)

// FiberNativeMiddlewareRegistrar is an optional Fiber v2 server capability.
// UseNative installs global native middleware before any endpoint, static or
// miss declaration. UseNativeTerminal queues middleware after all those routes,
// before sealing and startup validation. Neither method selects raw hosting.
//
// Register during single-owner construction, before any initialization or start
// attempt. Slices are copied; closure state remains caller-owned. Empty calls are
// no-ops while registration is open. Nil handlers reject the entire call without
// mutation. Native registration hooks must not reenter registration or startup.
// Ownership failures (including hook panics) seal registration, persist through
// initialization and prevent serving. These APIs do not admit preexisting
// unowned native routes.
type FiberNativeMiddlewareRegistrar interface {
	UseNative(handlers ...f2.Handler) error
	UseNativeTerminal(handlers ...f2.Handler) error
}

// FiberV3NativeMiddlewareRegistrar is the native Fiber v3 equivalent of
// FiberNativeMiddlewareRegistrar, with the same ordering and lifetime contract.
type FiberV3NativeMiddlewareRegistrar interface {
	UseNative(handlers ...f3.Handler) error
	UseNativeTerminal(handlers ...f3.Handler) error
}

var (
	// ErrNativeMiddlewareOrder rejects early middleware declared after routes.
	ErrNativeMiddlewareOrder = errors.New("native middleware must precede endpoint, static and miss declarations")
	// ErrNativeMiddlewareNil rejects a list containing a nil native handler.
	ErrNativeMiddlewareNil = errors.New("native middleware handler is nil")
)

var _ FiberNativeMiddlewareRegistrar = (*FiberAdapter)(nil)
var _ FiberV3NativeMiddlewareRegistrar = (*FiberV3Adapter)(nil)

// Owned by the root, shared by all groups. The atomic gate closes synchronously
// on startup entry, even when managed initialization will happen asynchronously.
// Other fields are protected by registrationMu; callbacks run under the same
// construction/finalization ownership as native endpoint registration.
type nativeMiddlewareRegistration struct {
	closed   atomic.Bool
	err      error
	terminal []func() error
}

func (a *FiberAdapter) UseNative(handlers ...f2.Handler) error {
	return registerNativeMiddleware(a.router.root, false, handlers, a.useNativeMiddleware)
}

func (a *FiberAdapter) UseNativeTerminal(handlers ...f2.Handler) error {
	return registerNativeMiddleware(a.router.root, true, handlers, a.useNativeMiddleware)
}

func (a *FiberAdapter) useNativeMiddleware(handler f2.Handler) error {
	return registerOwnedNativeMiddleware(a.router.ownership, a.app.Config().RequestMethods, handler, func() { a.app.Use(handler) })
}

func (a *FiberV3Adapter) UseNative(handlers ...f3.Handler) error {
	return registerNativeMiddleware(a.router.root, false, handlers, a.useNativeMiddleware)
}

func (a *FiberV3Adapter) UseNativeTerminal(handlers ...f3.Handler) error {
	return registerNativeMiddleware(a.router.root, true, handlers, a.useNativeMiddleware)
}

func (a *FiberV3Adapter) useNativeMiddleware(handler f3.Handler) error {
	return registerOwnedNativeMiddleware(a.router.ownership, a.app.Config().RequestMethods, handler, func() { a.app.Use(handler) })
}

func registerNativeMiddleware[H f2.Handler | f3.Handler](root *routerRoot, terminal bool, handlers []H, register func(H) error) error {
	operation := "register native middleware"
	if terminal {
		operation = "register terminal native middleware"
	}
	// Avoid waiting for a finalizer's native hooks when registration is already
	// closed. This also lets startup validators inspect the rejection safely.
	if root.nativeMiddleware.closed.Load() {
		return newRegistrationError(operation, "", "", RegistrationSealed, ErrRouterSealed)
	}
	root.registrationMu.Lock()
	defer root.registrationMu.Unlock()
	if root.nativeMiddleware.closed.Load() || root.registrationState() != RegistrationCollecting {
		return newRegistrationError(operation, "", "", root.registrationState(), ErrRouterSealed)
	}
	if root.nativeMiddleware.err != nil {
		return root.nativeMiddleware.err
	}
	for _, handler := range handlers {
		if handler == nil {
			return newRegistrationError(operation, "", "", root.registrationState(), ErrNativeMiddlewareNil)
		}
	}
	if len(handlers) == 0 {
		return nil
	}
	if !terminal && (len(root.routes) != 0 || len(root.lateRoutes) != 0 || len(root.missHandlers) != 0) {
		return newRegistrationError(operation, "", "", root.registrationState(), ErrNativeMiddlewareOrder)
	}
	for _, handler := range slices.Clone(handlers) {
		if terminal {
			root.nativeMiddleware.terminal = append(root.nativeMiddleware.terminal, func() error { return register(handler) })
		} else if err := register(handler); err != nil {
			return root.failNativeMiddleware(err)
		}
	}
	root.revision++
	return nil
}

func registerOwnedNativeMiddleware(guard *nativeRegistrationGuard, methods []string, handler any, register func()) (err error) {
	defer func() {
		if value := recover(); value != nil {
			guard.dirty = true
			err = errors.Join(ErrManagedNativeRegistration, &ManagedPanicError{Value: value})
		}
	}()
	if err := guard.validate(); err != nil {
		return err
	}
	methods = slices.Clone(methods)
	register()
	guard.afterAdd(methods, "/", handler, true)
	return guard.validate()
}

// Called with registrationMu held. Failure is sticky even on legacy/raw paths.
func (root *routerRoot) failNativeMiddleware(err error) error {
	if root.nativeMiddleware.err == nil {
		root.nativeMiddleware.err = &StartupValidationError{Err: err}
		root.startupValidationError = root.nativeMiddleware.err.Error()
		// Fiber may still hold an internal lock after a hook panic. Prevent all
		// further declarations, including immediate endpoints, from reentering it.
		root.nativeMiddleware.closed.Store(true)
		root.state = RegistrationSealed
		root.revision++
	}
	return root.nativeMiddleware.err
}

func (root *routerRoot) registerNativeTerminal() error {
	defer func() { root.nativeMiddleware.terminal = nil }()
	for _, register := range root.nativeMiddleware.terminal {
		if err := register(); err != nil {
			return root.failNativeMiddleware(err)
		}
	}
	return nil
}
