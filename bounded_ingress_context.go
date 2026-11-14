package router

import (
	"context"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

type ingressContextKey struct{}
type ingressLifetime struct {
	context.Context
	id string
}
type ingressContext struct {
	context.Context
	lifetime *ingressLifetime
}

func (c *ingressContext) AfterFunc(f func()) func() bool {
	return context.AfterFunc(c.Context, f)
}

func (c *ingressContext) Value(key any) any {
	switch key.(type) {
	case ingressRequestIDKey:
		return c.lifetime.id
	case ingressContextKey:
		return c.lifetime
	default:
		return c.Context.Value(key)
	}
}
func newIngressContext(ctx context.Context, id string) context.Context {
	return &ingressContext{Context: ctx, lifetime: &ingressLifetime{Context: ctx, id: id}}
}

// Keep application values, deadlines and cancellation while retaining the
// coordinator's upper lifetime bound. A retained Value marker alone is not
// evidence of cancellation ancestry: WithoutCancel deliberately preserves it.
func (life *ingressLifetime) withContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if owned, ok := ctx.(*ingressContext); ok && owned.lifetime == life {
		return ctx
	}
	if ctx.Done() != life.Done() {
		ctx = joinIngressCancellation(life.Context, ctx)
	}
	return &ingressContext{Context: ctx, lifetime: life}
}
func preserveIngressContext(previous, replacement context.Context) context.Context {
	if previous != nil {
		if life, ok := previous.Value(ingressContextKey{}).(*ingressLifetime); ok {
			return life.withContext(replacement)
		}
	}
	if replacement == nil {
		return context.Background()
	}
	return replacement
}
func nativeIngressContext(native *fasthttp.RequestCtx, values context.Context) context.Context {
	if life, ok := native.UserValue(ingressContextKey{}).(*ingressLifetime); ok {
		return life.withContext(values)
	}
	return values
}

// No request/native context is retained. AfterFunc registrations are removed
// when either source ends, including when the application parent is long-lived.
// The inner cancel context owns its own Cause key; delegating that private key
// to a different parent would break standard-library cancellation propagation.
type ingressCancellationContext struct {
	context.Context
	owned, application context.Context
	cancel             context.CancelCauseFunc
	mu                 sync.Mutex
	err                error
	stops              []func() bool
	done               chan struct{}
}

func joinIngressCancellation(owned, application context.Context) context.Context {
	inner, cancel := context.WithCancelCause(context.WithoutCancel(application))
	c := &ingressCancellationContext{Context: inner, owned: owned, application: application, cancel: cancel, done: make(chan struct{})}
	for _, source := range []context.Context{owned, application} {
		stop := context.AfterFunc(source, func() { c.finish(source) })
		c.mu.Lock()
		if c.err == nil {
			c.stops = append(c.stops, stop)
			c.mu.Unlock()
		} else {
			c.mu.Unlock()
			stop()
		}
	}
	c.check()
	return c
}
func (c *ingressCancellationContext) finish(source context.Context) {
	err := source.Err()
	if err == nil {
		return
	}
	cause := context.Cause(source)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	stops := c.stops
	c.stops = nil
	// Publish the canonical error and close Done atomically to readers of Err.
	c.cancel(cause)
	close(c.done)
	c.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}
func (c *ingressCancellationContext) check() {
	c.finish(c.owned)
	c.finish(c.application)
}
func (c *ingressCancellationContext) Done() <-chan struct{} {
	c.check()
	return c.done
}

// Use the public AfterFunc protocol for descendants. A distinct Done channel
// prevents context's internal cancelCtx fast path from replacing a propagated
// DeadlineExceeded with the inner manual-cancel context's Canceled error.
func (c *ingressCancellationContext) AfterFunc(f func()) func() bool {
	return context.AfterFunc(c.Context, func() {
		// Inner cancellation publishes Cause before the joined Done channel.
		// A scheduled callback must observe both publications before it runs.
		<-c.done
		f()
	})
}
func (c *ingressCancellationContext) Err() error {
	c.check()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
func (c *ingressCancellationContext) Deadline() (time.Time, bool) {
	a, aok := c.owned.Deadline()
	b, bok := c.application.Deadline()
	if aok && (!bok || a.Before(b)) {
		return a, true
	}
	return b, bok
}
