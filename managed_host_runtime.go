package router

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"

	"github.com/valyala/fasthttp"
)

type managedDriver struct {
	ingress     *fiberIngress
	initialize  func() error
	validate    func() error
	prepare     func() *fasthttp.Server
	serve       func(net.Listener) error
	withContext func(*fasthttp.RequestCtx, context.Context, fasthttp.RequestHandler)
	// Internal seam for deterministic bind/cancel tests; production uses ListenConfig.
	listen func(context.Context, string, string) (net.Listener, error)
}

type managedRun struct {
	streamCancels                                map[net.Conn]context.CancelFunc
	handlerID, connStateID                       uintptr
	mu                                           sync.Mutex
	state                                        ManagedState
	address                                      string
	listener                                     net.Listener
	server                                       *fasthttp.Server
	err                                          error
	stopErr                                      error
	forced                                       bool
	connections                                  map[net.Conn]struct{}
	requests                                     sync.WaitGroup
	conns                                        sync.WaitGroup
	parent                                       context.Context
	startup                                      context.Context
	cancelStartup                                context.CancelFunc
	requestContext                               context.Context
	cancelRequests                               context.CancelFunc
	options                                      ManagedOptions
	driver                                       managedDriver
	bound, serving, stopping, done, stopFinished chan struct{}
	startupDone, watcherDone                     chan struct{}
}

func newManagedRun(parent context.Context, options ManagedOptions, driver managedDriver) *managedRun {
	startup, cancelStartup := context.WithCancel(parent)
	requests, cancelRequests := context.WithCancel(context.WithoutCancel(parent))
	if driver.listen == nil {
		driver.listen = (&net.ListenConfig{}).Listen
	}
	return &managedRun{state: ManagedCreated, parent: parent, startup: startup, cancelStartup: cancelStartup,
		requestContext: requests, cancelRequests: cancelRequests, options: options, driver: driver,
		connections: make(map[net.Conn]struct{}), streamCancels: make(map[net.Conn]context.CancelFunc), bound: make(chan struct{}), serving: make(chan struct{}),
		stopping: make(chan struct{}), done: make(chan struct{}), stopFinished: make(chan struct{}),
		startupDone: make(chan struct{}), watcherDone: make(chan struct{})}
}
func (r *managedRun) Bound() <-chan struct{}    { return r.bound }
func (r *managedRun) Serving() <-chan struct{}  { return r.serving }
func (r *managedRun) Stopping() <-chan struct{} { return r.stopping }
func (r *managedRun) Done() <-chan struct{}     { return r.done }
func (r *managedRun) Address() string           { r.mu.Lock(); defer r.mu.Unlock(); return r.address }
func (r *managedRun) State() ManagedState       { r.mu.Lock(); defer r.mu.Unlock(); return r.state }
func (r *managedRun) Stop(ctx context.Context) error {
	r.requestStop(nil)
	if ctx == nil {
		return ErrManagedOptions
	}
	select {
	case <-r.stopFinished:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *managedRun) Wait(ctx context.Context) error {
	if ctx == nil {
		return ErrManagedOptions
	}
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *managedRun) addError(err error) {
	if err != nil {
		r.mu.Lock()
		r.err = errors.Join(r.err, err)
		r.mu.Unlock()
	}
}
func managedCall(phase string, callback func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = &ManagedError{Phase: phase, Err: &ManagedPanicError{Value: value}}
		}
	}()
	if err = callback(); err != nil {
		err = &ManagedError{Phase: phase, Err: err}
	}
	return err
}
func (r *managedRun) start() {
	go func() {
		defer close(r.watcherDone)
		select {
		case <-r.parent.Done():
			r.requestStop(context.Cause(r.parent))
		case <-r.stopping:
		}
	}()
	go func() {
		defer close(r.startupDone)
		err := managedCall("startup", r.run)
		r.addError(err)
		r.requestStop(nil)
	}()
}
func (r *managedRun) canceled() bool {
	if cause := context.Cause(r.parent); cause != nil {
		r.requestStop(cause)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == ManagedStopping || r.state == ManagedStopped
}
func (r *managedRun) run() error {
	if r.canceled() {
		return nil
	}
	r.mu.Lock()
	if r.state == ManagedCreated {
		r.state = ManagedInitializing
	}
	r.mu.Unlock()
	if err := managedCall("initialize", r.driver.initialize); err != nil {
		return err
	}
	if r.canceled() {
		return nil
	}
	if err := managedCall("registration", r.driver.validate); err != nil {
		return err
	}
	server := r.driver.prepare()
	if err := managedCall("registration", r.driver.validate); err != nil {
		return err
	}
	r.mu.Lock()
	r.server = server
	r.mu.Unlock()
	handler, connState := server.Handler, server.ConnState
	var ingress *ingressRuntime
	if r.driver.ingress != nil {
		ingress = r.driver.ingress.runtime
	}
	server.Handler = func(c *fasthttp.RequestCtx) {
		r.canceled()
		r.mu.Lock()
		if r.state != ManagedServing {
			r.mu.Unlock()
			if ingress != nil {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				ingress.handle(c, ctx, false)
			} else {
				c.SetStatusCode(fasthttp.StatusServiceUnavailable)
				c.SetConnectionClose()
			}
			return
		}
		r.requests.Add(1)
		r.mu.Unlock()
		defer r.requests.Done()
		ctx, cancel := context.WithCancel(r.requestContext)
		defer func() {
			// Fiber writes body streams after its handler returns. Keep their context
			// until the connection becomes idle/closed; never retain the pooled Ctx.
			if c.Response.IsBodyStream() {
				r.mu.Lock()
				r.streamCancels[c.Conn()] = cancel
				r.mu.Unlock()
			} else {
				cancel()
			}
		}()
		if ingress != nil {
			ingress.handle(c, ctx, true)
			// Flush the admitted response before native shutdown can close an idle
			// connection, including when another request is already pipelined.
			if r.canceled() {
				c.SetConnectionClose()
			}
		} else {
			r.driver.withContext(c, ctx, handler)
		}
	}
	server.ConnState = func(conn net.Conn, state fasthttp.ConnState) {
		terminal := false
		var cancelStream context.CancelFunc
		r.mu.Lock()
		if state == fasthttp.StateIdle || state == fasthttp.StateClosed || state == fasthttp.StateHijacked {
			cancelStream = r.streamCancels[conn]
			delete(r.streamCancels, conn)
		}
		switch state {
		case fasthttp.StateNew:
			if _, exists := r.connections[conn]; !exists {
				r.connections[conn] = struct{}{}
				r.conns.Add(1)
			}
		case fasthttp.StateClosed, fasthttp.StateHijacked:
			if _, exists := r.connections[conn]; exists {
				delete(r.connections, conn)
				terminal = true
			}
		}
		force := r.forced && state != fasthttp.StateHijacked && state != fasthttp.StateClosed
		r.mu.Unlock()
		if cancelStream != nil {
			cancelStream()
		}
		if terminal {
			defer r.conns.Done()
		}
		if force {
			_ = conn.Close()
		}
		if connState != nil {
			connState(conn, state)
		}
	}
	r.handlerID = reflect.ValueOf(server.Handler).Pointer()
	r.connStateID = reflect.ValueOf(server.ConnState).Pointer()
	if r.canceled() {
		return nil
	}
	ln, err := r.driver.listen(r.startup, r.options.Network, r.options.Address)
	if err != nil {
		if r.canceled() && errors.Is(err, context.Canceled) {
			return nil
		}
		return &ManagedError{Phase: "bind", Err: err}
	}
	// Publication and stop share one lock. A listener acquired after stop is closed
	// without ever publishing Bound or Serving.
	if r.canceled() {
		_ = ln.Close()
		return nil
	}
	r.mu.Lock()
	if r.state == ManagedStopping {
		r.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	r.listener = ln
	r.address = ln.Addr().String()
	r.state = ManagedBound
	close(r.bound)
	r.mu.Unlock()
	defer ln.Close()
	if r.options.OnBound != nil {
		if err := managedCall("on-bound", func() error { return r.options.OnBound(r.startup, ln.Addr().String()) }); err != nil {
			return err
		}
	}
	if r.canceled() {
		return nil
	}
	return managedCall("native-serve", func() error {
		err := r.driver.serve(&managedListener{Listener: ln, run: r})
		// Only suppress the expected closed-listener return from native serving.
		// Errors returned by application callbacks remain inspectable after Stop.
		if errors.Is(err, net.ErrClosed) && r.canceled() {
			return nil
		}
		return err
	})
}

type managedListener struct {
	net.Listener
	run  *managedRun
	once sync.Once
	err  error
}

func (l *managedListener) Accept() (net.Conn, error) {
	r := l.run
	l.once.Do(func() {
		// Native hooks have now completed. Recheck their registration effects before
		// publishing readiness or accepting any connection.
		l.err = managedCall("registration", r.driver.validate)
		if reflect.ValueOf(r.server.Handler).Pointer() != r.handlerID || reflect.ValueOf(r.server.ConnState).Pointer() != r.connStateID {
			l.err = errors.Join(l.err, ErrManagedUnsupported)
		}
		if l.err != nil {
			r.addError(l.err)
			r.requestStop(nil)
		}
	})
	if l.err != nil {
		return nil, net.ErrClosed
	}
	if r.canceled() {
		return nil, net.ErrClosed
	}
	r.mu.Lock()
	if r.state == ManagedStopping || r.state == ManagedStopped {
		r.mu.Unlock()
		return nil, net.ErrClosed
	}
	if r.state == ManagedBound {
		r.state = ManagedServing
		close(r.serving)
	}
	r.mu.Unlock()
	return l.Listener.Accept()
}

func (r *managedRun) requestStop(cause error) {
	r.mu.Lock()
	if r.state == ManagedStopping || r.state == ManagedStopped {
		// The first stop fixes the cancellation cause; late startup/callback
		// failures are recorded separately by addError.
		r.mu.Unlock()
		return
	}
	if parentCause := context.Cause(r.parent); parentCause != nil {
		cause = parentCause
	}
	if cause != nil {
		r.err = errors.Join(r.err, cause)
	}
	r.state = ManagedStopping
	close(r.stopping)
	ln := r.listener
	// Establish one deadline at the stop transition, not when a worker gets CPU.
	drain, cancel := context.WithTimeout(context.WithoutCancel(r.parent), r.options.DrainTimeout)
	r.mu.Unlock()
	r.cancelStartup()
	if ln != nil {
		_ = ln.Close()
	}
	go r.drain(drain, cancel)
}
func (r *managedRun) drain(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	hookDone := make(chan struct{})
	go func() {
		defer close(hookDone)
		if r.options.OnShutdown != nil {
			r.addError(managedCall("on-shutdown", func() error { return r.options.OnShutdown(ctx) }))
		}
	}()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		// Never invoke native shutdown before native Serve has registered its
		// listener, unless startup has already exited and cannot publish it later.
		select {
		case <-r.serving:
		case <-r.startupDone:
		}
		r.mu.Lock()
		server := r.server
		r.mu.Unlock()
		if server != nil {
			err := server.ShutdownWithContext(ctx)
			if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.DeadlineExceeded) {
				r.addError(&ManagedError{Phase: "drain", Err: err})
			}
		}
		<-r.startupDone
		r.requests.Wait()
		r.conns.Wait()
	}()
	joined := make(chan struct{})
	go func() { <-drained; <-hookDone; <-r.watcherDone; close(joined) }()
	timedOut := false
	select {
	case <-joined:
		timedOut = ctx.Err() != nil
	case <-ctx.Done():
		timedOut = true
	}
	if !timedOut {
		r.cancelRequests()
		r.mu.Lock()
		r.stopErr = r.err
		r.state = ManagedStopped
		close(r.stopFinished)
		close(r.done)
		r.mu.Unlock()
		return
	}
	r.addError(&ManagedError{Phase: "drain", Err: errors.Join(ErrManagedDrainTimeout, context.DeadlineExceeded)})
	r.cancelRequests()
	r.mu.Lock()
	r.forced = true
	connections := make([]net.Conn, 0, len(r.connections))
	for conn := range r.connections {
		connections = append(connections, conn)
	}
	r.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
	r.mu.Lock()
	r.stopErr = r.err
	close(r.stopFinished)
	r.mu.Unlock()

	<-joined
	r.mu.Lock()
	r.state = ManagedStopped
	close(r.done)
	r.mu.Unlock()
}
