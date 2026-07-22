package router

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type interruptTestConn struct {
	deadlineErr   error
	closeErr      error
	deadlineCalls atomic.Int32
	closeCalls    atomic.Int32
}

func (c *interruptTestConn) Read([]byte) (int, error)         { return 0, net.ErrClosed }
func (c *interruptTestConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *interruptTestConn) Close() error                     { c.closeCalls.Add(1); return c.closeErr }
func (c *interruptTestConn) LocalAddr() net.Addr              { return interruptTestAddr("local") }
func (c *interruptTestConn) RemoteAddr() net.Addr             { return interruptTestAddr("remote") }
func (c *interruptTestConn) SetDeadline(time.Time) error      { return nil }
func (c *interruptTestConn) SetWriteDeadline(time.Time) error { return nil }
func (c *interruptTestConn) SetReadDeadline(time.Time) error {
	c.deadlineCalls.Add(1)
	return c.deadlineErr
}

type interruptTestAddr string

func (a interruptTestAddr) Network() string { return "test" }
func (a interruptTestAddr) String() string  { return string(a) }

type interruptUnsafeConn struct {
	*interruptTestConn
	raw net.Conn
}

func (c *interruptUnsafeConn) UnsafeConn() net.Conn { return c.raw }

func TestInterruptWebSocketReadUsesDeadlineWithoutClosingHealthyConnection(t *testing.T) {
	conn := &interruptTestConn{}
	if err := interruptWebSocketRead(conn); err != nil {
		t.Fatalf("interrupt error = %v, want nil", err)
	}
	if got := conn.deadlineCalls.Load(); got != 1 {
		t.Fatalf("read deadline calls = %d, want 1", got)
	}
	if got := conn.closeCalls.Load(); got != 0 {
		t.Fatalf("close calls = %d, want 0", got)
	}
}

func TestInterruptWebSocketReadClosesUnsafeRawConnectionAfterDeadlineFailure(t *testing.T) {
	deadlineErr := errors.New("forced deadline failure")
	wrapper := &interruptTestConn{deadlineErr: deadlineErr}
	raw := &interruptTestConn{}
	conn := &interruptUnsafeConn{interruptTestConn: wrapper, raw: raw}

	err := interruptWebSocketRead(conn)
	if !errors.Is(err, deadlineErr) {
		t.Fatalf("interrupt error = %v, want deadline failure", err)
	}
	if got := wrapper.closeCalls.Load(); got != 0 {
		t.Fatalf("wrapper close calls = %d, want 0", got)
	}
	if got := raw.closeCalls.Load(); got != 1 {
		t.Fatalf("raw close calls = %d, want 1", got)
	}
}

func TestInterruptWebSocketReadJoinsDeadlineAndCloseFailures(t *testing.T) {
	deadlineErr := errors.New("forced deadline failure")
	closeErr := errors.New("forced close failure")
	conn := &interruptTestConn{deadlineErr: deadlineErr, closeErr: closeErr}

	err := interruptWebSocketRead(conn)
	if !errors.Is(err, deadlineErr) || !errors.Is(err, closeErr) {
		t.Fatalf("interrupt error = %v, want joined deadline and close failures", err)
	}
}
