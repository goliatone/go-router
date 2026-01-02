package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
)

type patternReader struct {
	pattern   []byte
	remaining int64
}

func (r *patternReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if len(r.pattern) == 0 {
		return 0, io.EOF
	}

	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}

	for i := 0; i < n; i++ {
		p[i] = r.pattern[i%len(r.pattern)]
	}

	r.remaining -= int64(n)
	return n, nil
}

func TestDownloadResponder_WriteStream_UsesSendStream(t *testing.T) {
	ctx := NewMockContext()
	ctx.On("SetHeader", mock.Anything, mock.Anything).Return()
	ctx.On("Status", mock.Anything).Return(ctx)
	ctx.On("SendStream", mock.Anything).Return(nil)

	responder := NewDownloadResponder(ctx)
	err := responder.WriteStream(context.Background(), "text/plain", strings.NewReader("hello"), WithFilename("file.txt"))
	if err != nil {
		t.Fatalf("WriteStream error: %v", err)
	}

	ctx.AssertCalled(t, "SendStream", mock.Anything)
	ctx.AssertNotCalled(t, "Send", mock.Anything)
}

func TestDownloadResponder_FiberLargeStreamHeaders(t *testing.T) {
	adapter := NewFiberAdapter()
	r := adapter.Router()

	size := int64(1024 * 1024)
	r.Get("/download", func(c Context) error {
		responder := NewDownloadResponder(c)
		payload := DownloadPayload{
			ContentType: "text/csv",
			Filename:    "export.csv",
			ExportID:    "exp-1",
			Size:        size,
			Reader: &patternReader{
				pattern:   []byte("abc123"),
				remaining: size,
			},
		}
		return responder.WriteDownload(c.Context(), payload)
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("Error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	if got := resp.Header.Get("Content-Disposition"); got != "attachment; filename=\"export.csv\"" {
		t.Fatalf("Expected Content-Disposition header, got %q", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/csv" {
		t.Fatalf("Expected Content-Type text/csv, got %q", got)
	}
	if got := resp.Header.Get("X-Export-Id"); got != "exp-1" {
		t.Fatalf("Expected X-Export-Id exp-1, got %q", got)
	}
	if got := resp.Header.Get("Content-Length"); got != fmt.Sprintf("%d", size) {
		t.Fatalf("Expected Content-Length %d, got %q", size, got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	if int64(len(body)) != size {
		t.Fatalf("Expected body length %d, got %d", size, len(body))
	}
}
