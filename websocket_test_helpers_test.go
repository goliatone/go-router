package router_test

import (
	"net/http"
	"testing"

	"github.com/gorilla/websocket"
)

func closeWebSocketResponse(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return
	}
	if err := resp.Body.Close(); err != nil {
		t.Logf("websocket response body close failed: %v", err)
	}
}

func closeWebSocketConn(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if conn == nil {
		return
	}
	if err := conn.Close(); err != nil {
		t.Logf("websocket close failed: %v", err)
	}
}
