package router

import (
	"context"
	"errors"
	"io"
	"net/http"
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

	options := ResolveStreamOptions(opts...)
	if contentType == "" {
		contentType = defaultDownloadContentType
	}

	applyDownloadHeaders(c, contentType, options)

	ctx := c.liveCtx()
	if ctx == nil {
		return errors.New("context unavailable")
	}

	size := -1
	if options.ContentLength > 0 {
		maxInt := int64(^uint(0) >> 1)
		if options.ContentLength <= maxInt {
			size = int(options.ContentLength)
		}
	}

	ctx.Context().SetBodyStream(r, size)
	ctx.Status(http.StatusOK)
	return nil
}

func (c *httpRouterContext) WriteDownload(ctx context.Context, payload DownloadPayload) error {
	return writeDownload(c, ctx, payload)
}

func (c *httpRouterContext) WriteStream(_ context.Context, contentType string, r io.Reader, opts ...StreamOption) error {
	return writeStream(c, contentType, r, opts...)
}
