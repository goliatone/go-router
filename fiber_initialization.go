package router

import "sync"

// One initializer publishes one sticky result. The mutex never spans user code.
// Unexpected panics are cached for waiters and rethrown to the initiating legacy
// caller; managed hosting catches them at its asynchronous startup boundary.
type fiberInitialization struct {
	mu   sync.Mutex
	done chan struct{}
	err  error
}

func (i *fiberInitialization) do(initialize func() error) (err error) {
	i.mu.Lock()
	if i.done != nil {
		done := i.done
		i.mu.Unlock()
		<-done
		return i.err
	}
	i.done = make(chan struct{})
	i.mu.Unlock()
	defer func() {
		if value := recover(); value != nil {
			i.err = &StartupValidationError{Err: &ManagedPanicError{Value: value}}
			close(i.done)
			panic(value)
		}
		i.err = err
		close(i.done)
	}()
	return initialize()
}
