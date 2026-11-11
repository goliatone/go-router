package router

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// StartupValidatorRegistrar is an optional capability of native Fiber routers.
// Groups register against the same root. Register before initialization; callbacks
// run in name order after routes are sealed and before serving. A callback may
// inspect snapshots but must not mutate registration or recursively start the server.
// No validators are run by diagnostic reads.
type StartupValidatorRegistrar interface {
	RegisterStartupValidator(name string, validate func() error) error
}

// StartupInitializer provides an error-returning alternative to compatibility Init.
// It finalizes routes and validates startup without opening a listener.
type StartupInitializer interface{ Initialize() error }

// StartupValidationError prevents serving a finalized but invalid configuration.
// Serve returns this error. Init and WrappedRouter panic with it, since those
// compatibility methods cannot return errors. Failures persist across attempts.
type StartupValidationError struct{ Err error }

func (e *StartupValidationError) Error() string {
	if e == nil || e.Err == nil {
		return "router startup validation failed"
	}
	return "router startup validation failed: " + e.Err.Error()
}
func (e *StartupValidationError) Unwrap() error { return e.Err }

func (r *FiberRouter) RegisterStartupValidator(name string, validate func() error) error {
	return r.root.registerStartupValidator(name, validate)
}
func (r *FiberV3Router) RegisterStartupValidator(name string, validate func() error) error {
	return r.root.registerStartupValidator(name, validate)
}

func (root *routerRoot) registerStartupValidator(name string, validate func() error) error {
	name = strings.TrimSpace(name)
	if name == "" || validate == nil {
		return fmt.Errorf("startup validator requires a name and callback")
	}
	root.registrationMu.Lock()
	defer root.registrationMu.Unlock()
	if root.registrationState() != RegistrationCollecting {
		return newRegistrationError("register startup validator", "", name, root.registrationState(), ErrRouterSealed)
	}
	if _, exists := root.startupValidators[name]; exists {
		return fmt.Errorf("startup validator %q already registered", name)
	}
	if root.startupValidators == nil {
		root.startupValidators = make(map[string]func() error)
	}
	root.startupValidators[name] = validate
	return nil
}

// Called by serialized adapter initialization only, after route publication.
func (root *routerRoot) validateStartup() error {
	root.registrationMu.Lock()
	callbacks := make(map[string]func() error, len(root.startupValidators))
	names := make([]string, 0, len(root.startupValidators))
	for name, callback := range root.startupValidators {
		names = append(names, name)
		callbacks[name] = callback
	}
	root.registrationMu.Unlock()
	sort.Strings(names)
	var failures []error
	for _, name := range names {
		if err := invokeStartupValidator(callbacks[name]); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
		}
	}
	if len(failures) == 0 {
		return nil
	}
	failure := &StartupValidationError{Err: errors.Join(failures...)}
	message := failure.Error()
	root.registrationMu.Lock()
	root.startupValidationError = message
	root.registrationMu.Unlock()
	return failure
}

func invokeStartupValidator(callback func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("validator panicked: %v", value)
		}
	}()
	return callback()
}
