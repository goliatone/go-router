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

func requireTestType[T any](t *testing.T, value any) T {
	t.Helper()
	typed, ok := value.(T)
	if !ok {
		t.Fatalf("unexpected fixture type %T", value)
	}
	return typed
}

func checkTestError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}

func testHTTPGet(t *testing.T, client *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
