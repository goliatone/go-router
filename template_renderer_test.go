package router_test

import (
	"bytes"
	"testing"

	"github.com/goliatone/go-router"
)

func TestAsTemplateRendererUnsupported(t *testing.T) {
	if renderer, ok := router.AsTemplateRenderer(nil); ok || renderer != nil {
		t.Fatalf("expected nil context not to support TemplateRenderer")
	}

	if renderer, ok := router.AsTemplateRenderer(&stubContext{}); ok || renderer != nil {
		t.Fatalf("expected unsupported context not to support TemplateRenderer")
	}
}

func TestMockContextTemplateRenderer(t *testing.T) {
	var _ router.TemplateRenderer = (*router.MockContext)(nil)

	ctx := router.NewMockContext()
	renderer, ok := router.AsTemplateRenderer(ctx)
	if !ok {
		t.Fatal("expected MockContext to support TemplateRenderer")
	}

	b, err := renderer.RenderToBytes("index", router.ViewContext{"title": "Test"})
	if err != nil {
		t.Fatalf("RenderToBytes failed: %v", err)
	}
	if string(b) != "rendered: index" {
		t.Fatalf("expected default mock render output, got %q", b)
	}
	if ctx.ResponseBodyM != "" {
		t.Fatalf("expected RenderToBytes not to mutate ResponseBodyM, got %q", ctx.ResponseBodyM)
	}
	if ctx.ResponseWritten() || ctx.ResponseBodySize() != 0 {
		t.Fatalf("expected RenderToBytes not to mutate response state, written=%v size=%d", ctx.ResponseWritten(), ctx.ResponseBodySize())
	}

	ctx.RenderBodyM = "<h1>direct</h1>"
	var out bytes.Buffer
	if err := renderer.RenderToWriter(&out, "index", nil); err != nil {
		t.Fatalf("RenderToWriter failed: %v", err)
	}
	if out.String() != "<h1>direct</h1>" {
		t.Fatalf("expected direct mock render output, got %q", out.String())
	}
	if ctx.ResponseBodyM != "" {
		t.Fatalf("expected RenderToWriter not to mutate ResponseBodyM, got %q", ctx.ResponseBodyM)
	}
	if ctx.ResponseWritten() || ctx.ResponseBodySize() != 0 {
		t.Fatalf("expected RenderToWriter not to mutate response state, written=%v size=%d", ctx.ResponseWritten(), ctx.ResponseBodySize())
	}
}
