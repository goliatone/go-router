package router

import (
	"context"
	"io"
	"net/http"

	"github.com/valyala/fasthttp"
)

var _ DownloadResponder = (*fiberV3Context)(nil)

func (c *fiberV3Context) WriteDownload(ctx context.Context, payload DownloadPayload) error {
	return writeDownload(c, ctx, payload)
}

func (c *fiberV3Context) WriteStream(_ context.Context, contentType string, r io.Reader, opts ...StreamOption) error {
	if c == nil || r == nil {
		return nil
	}
	var native *fasthttp.RequestCtx
	if ctx := c.liveCtx(); ctx != nil {
		native = ctx.RequestCtx()
	}
	if err := prepareFiberStream(c, native, contentType, r, opts...); err != nil {
		return err
	}
	c.statusCode = http.StatusOK
	c.written = true
	c.stream = true
	return nil
}
