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
			if !stream.closed || errors.Is(err, closeErr) {
				t.Fatalf("stream cleanup: closed=%t error=%v", stream.closed, err)
			}
			if failWrite && !errors.Is(err, writeErr) {
				t.Fatalf("lost stream write error: %v", err)
			}
			if !failWrite && err != nil {
				t.Fatalf("successful stream returned cleanup error: %v", err)
			}
			if !failWrite && recorder.Body.String() != "payload" {
				t.Fatalf("stream body: %q", recorder.Body.String())
			}
		})
	}
}

func TestHTTPRouterStreamCloseFailurePreservesResponse(t *testing.T) {
	for _, path := range []string{"/file", "/api/file"} {
		t.Run(path, func(t *testing.T) {
			server := router.NewHTTPServer()
			logger := &captureLogger{}
			routes := server.Router().WithLogger(logger)
			stream := &failingCloseStream{
				Reader:   strings.NewReader("payload"),
				closeErr: errors.New("storage-close-detail"),
			}
			routes.Get(path, func(c router.Context) error {
				c.SetHeader("Content-Type", "application/octet-stream")
				return c.SendStream(stream)
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
			server.WrappedRouter().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK || recorder.Body.String() != "payload" || recorder.Header().Get("Content-Type") != "application/octet-stream" {
				t.Fatalf("stream response changed: status=%d body=%q headers=%v", recorder.Code, recorder.Body.String(), recorder.Header())
			}
			if !stream.closed {
				t.Fatal("stream was not closed")
			}
			if len(logger.warns) != 1 || !strings.Contains(logger.warns[0], "storage-close-detail") {
				t.Fatalf("cleanup failure was not logged: %v", logger.warns)
			}
		})
	}
}
