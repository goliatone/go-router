package router

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A consumer depends on the optional capability, not a new required Server
// method. Concrete adapter conformance belongs to the implementation task.
func compileManagedConsumer(ctx context.Context, server ManagedServer) error {
	run, err := server.StartManaged(ctx, ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second})
	if err != nil {
		return err
	}
	select {
	case <-run.Serving():
		return run.Stop(ctx)
	case <-run.Done():
		return run.Wait(ctx)
	case <-ctx.Done():
		return run.Stop(ctx)
	}
}
func TestManagedOptionsContract(t *testing.T) {
	for _, o := range []ManagedOptions{{}, {Address: "a"}, {Address: "a", DrainTimeout: -1}, {Address: "a", DrainTimeout: 1, Network: "unix"}} {
		if _, err := o.validated(); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	o, err := (ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second}).validated()
	if err != nil || o.Network != "tcp" {
		t.Fatalf("defaults: %+v %v", o, err)
	}
	cause := errors.New("cause")
	err = &ManagedError{Phase: "callback", Err: &ManagedPanicError{Value: cause}}
	var panicErr *ManagedPanicError
	if !errors.Is(err, cause) || !errors.As(err, &panicErr) {
		t.Fatal("lost typed cause")
	}
}
