package router

import (
	"testing"

	fiber3 "github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

func TestFiberV3SnapshotRawHeadersAndLocalOwnership(t *testing.T) {
	app := fiber3.New()
	request := &fasthttp.RequestCtx{}
	request.Request.Header.DisableNormalizing()
	request.Request.Header.Set("x-aCtOr", "alice")
	native := app.AcquireCtx(request)
	defer app.ReleaseCtx(native)
	base := NewFiberV3Context(native, nil).(*fiberV3Context)
	native.Locals(fiberV3MiddlewareKey{}, &fiberV3MiddlewareState{routerCtx: base})
	buffer := []byte("original")
	native.Locals("buffer", buffer)
	base.captureRequestMeta()
	base.snapshotLocals(native)
	base.ctx = nil
	buffer[0] = 'X'
	request.Request.Header.Set("x-aCtOr", "other")
	for _, key := range []string{"X-Actor", "x-actor", "X-ACTOR"} {
		if base.Header(key) != "alice" {
			t.Fatalf("header %q = %q", key, base.Header(key))
		}
	}
	if got := string(base.Locals("buffer").([]byte)); got != "original" {
		t.Fatalf("request-backed buffer retained: %q", got)
	}
	if base.Locals(fiberV3MiddlewareKey{}) != nil {
		t.Fatal("native dispatch state retained")
	}
}
