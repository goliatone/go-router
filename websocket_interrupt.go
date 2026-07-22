package router

import (
	"errors"
	"fmt"
	"net"
	"time"
)

type unsafeNetConner interface {
	UnsafeConn() net.Conn
}

// interruptWebSocketRead works below the WebSocket framing layer. net.Conn
// explicitly permits concurrent method calls, unlike the Gorilla-compatible
// WebSocket APIs, where SetReadDeadline belongs to the single-reader contract.
func interruptWebSocketRead(conn net.Conn) error {
	if conn == nil {
		return fmt.Errorf("interrupt websocket read: underlying connection unavailable")
	}

	deadlineErr := conn.SetReadDeadline(time.Now())
	if deadlineErr == nil {
		return nil
	}

	// fasthttp's hijackConn.Close can intentionally be a no-op while the
	// server owns connection cleanup. Its UnsafeConn exposes the actual socket,
	// which is the required fallback when setting a deadline fails.
	closeConn := conn
	if unsafeConn, ok := conn.(unsafeNetConner); ok {
		if raw := unsafeConn.UnsafeConn(); raw != nil {
			closeConn = raw
		}
	}
	if closeErr := closeConn.Close(); closeErr != nil {
		return errors.Join(deadlineErr, closeErr)
	}
	return deadlineErr
}
