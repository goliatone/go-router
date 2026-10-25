package router_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	router "github.com/goliatone/go-router"
)

type failingCloseStream struct {
	io.Reader
	closeErr error
	closed   bool
}

func (s *failingCloseStream) Close() error {
	s.closed = true
	return s.closeErr
}

type failingStreamWriter struct {
	http.ResponseWriter
	writeErr error
}

func (w failingStreamWriter) Write([]byte) (int, error) { return 0, w.writeErr }

func TestHTTPRouterStreamCleanupErrors(t *testing.T) {
	closeErr := errors.New("stream close failed")
	writeErr := errors.New("stream write failed")
	for _, failWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "close", true: "write-and-close"}[failWrite], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			var writer http.ResponseWriter = recorder
			if failWrite {
				writer = failingStreamWriter{ResponseWriter: recorder, writeErr: writeErr}
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/stream", nil)
			ctx := router.NewHTTPRouterContext(writer, request, nil, nil)
			stream := &failingCloseStream{Reader: strings.NewReader("payload"), closeErr: closeErr}
			err := ctx.SendStream(stream)
			if !stream.closed || !errors.Is(err, closeErr) {
				t.Fatalf("stream cleanup: closed=%t error=%v", stream.closed, err)
			}
			if failWrite && !errors.Is(err, writeErr) {
				t.Fatalf("lost stream write error: %v", err)
			}
			if !failWrite && recorder.Body.String() != "payload" {
				t.Fatalf("stream body: %q", recorder.Body.String())
			}
		})
	}
}
