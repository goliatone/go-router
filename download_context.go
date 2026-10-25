package router

import (
	"context"
	"io"
	"net/http"

	"github.com/valyala/fasthttp"
)

var _ DownloadResponder = (*fiberContext)(nil)
var _ DownloadResponder = (*httpRouterContext)(nil)

func (c *fiberContext) WriteDownload(ctx context.Context, payload DownloadPayload) error {
	return writeDownload(c, ctx, payload)
}

func (c *fiberContext) WriteStream(_ context.Context, contentType string, r io.Reader, opts ...StreamOption) error {
	if c == nil || r == nil {
		return nil
	}
	var native *fasthttp.RequestCtx
	if ctx := c.liveCtx(); ctx != nil {
		native = ctx.Context()
	}
	if err := prepareFiberStream(c, native, contentType, r, opts...); err != nil {
		return err
	}
	c.statusCode = http.StatusOK
	c.written = true
	c.stream = true
	return nil
}

func (c *httpRouterContext) WriteDownload(ctx context.Context, payload DownloadPayload) error {
	return writeDownload(c, ctx, payload)
}

func (c *httpRouterContext) WriteStream(_ context.Context, contentType string, r io.Reader, opts ...StreamOption) error {
	return writeStream(c, contentType, r, opts...)
}
