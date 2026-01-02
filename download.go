package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

const defaultDownloadContentType = "application/octet-stream"

// DownloadPayload describes a file download response.
type DownloadPayload struct {
	ContentType    string
	Filename       string
	ExportID       string
	Size           int64
	MaxBufferBytes int64
	Reader         io.Reader
	Bytes          []byte
}

// StreamOptions configures download stream responses.
type StreamOptions struct {
	Filename       string
	ExportID       string
	ContentLength  int64
	MaxBufferBytes int64
}

// StreamOption applies stream options.
type StreamOption func(*StreamOptions)

// WithFilename sets the download filename.
func WithFilename(filename string) StreamOption {
	return func(opts *StreamOptions) {
		opts.Filename = filename
	}
}

// WithExportID sets the export id response header.
func WithExportID(exportID string) StreamOption {
	return func(opts *StreamOptions) {
		opts.ExportID = exportID
	}
}

// WithContentLength sets the content length header.
func WithContentLength(length int64) StreamOption {
	return func(opts *StreamOptions) {
		if length > 0 {
			opts.ContentLength = length
		}
	}
}

// WithMaxBufferBytes sets the maximum buffer size when streaming is unavailable.
func WithMaxBufferBytes(max int64) StreamOption {
	return func(opts *StreamOptions) {
		if max > 0 {
			opts.MaxBufferBytes = max
		}
	}
}

// ResolveStreamOptions applies stream options.
func ResolveStreamOptions(opts ...StreamOption) StreamOptions {
	resolved := StreamOptions{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&resolved)
	}
	return resolved
}

// DownloadResponder provides streaming download helpers.
type DownloadResponder interface {
	WriteDownload(ctx context.Context, payload DownloadPayload) error
	WriteStream(ctx context.Context, contentType string, r io.Reader, opts ...StreamOption) error
}

type downloadResponder struct {
	ctx Context
}

// NewDownloadResponder returns a download responder bound to the given context.
func NewDownloadResponder(ctx Context) DownloadResponder {
	if responder, ok := ctx.(DownloadResponder); ok {
		return responder
	}
	return downloadResponder{ctx: ctx}
}

func (res downloadResponder) WriteDownload(ctx context.Context, payload DownloadPayload) error {
	return writeDownload(res.ctx, ctx, payload)
}

func (res downloadResponder) WriteStream(_ context.Context, contentType string, r io.Reader, opts ...StreamOption) error {
	return writeStream(res.ctx, contentType, r, opts...)
}

func writeDownload(target Context, ctx context.Context, payload DownloadPayload) error {
	if target == nil {
		return nil
	}
	if payload.Reader == nil && payload.Bytes == nil {
		return nil
	}

	reader := payload.Reader
	size := payload.Size
	if reader == nil && payload.Bytes != nil {
		reader = bytes.NewReader(payload.Bytes)
		if size == 0 {
			size = int64(len(payload.Bytes))
		}
	}

	opts := []StreamOption{
		WithFilename(payload.Filename),
		WithExportID(payload.ExportID),
		WithContentLength(size),
		WithMaxBufferBytes(payload.MaxBufferBytes),
	}

	if responder, ok := target.(DownloadResponder); ok {
		return responder.WriteStream(ctx, payload.ContentType, reader, opts...)
	}
	return writeStream(target, payload.ContentType, reader, opts...)
}

func writeStream(target Context, contentType string, r io.Reader, opts ...StreamOption) error {
	if target == nil || r == nil {
		return nil
	}

	options := ResolveStreamOptions(opts...)
	if contentType == "" {
		contentType = defaultDownloadContentType
	}

	applyDownloadHeaders(target, contentType, options)
	target.Status(http.StatusOK)
	return target.SendStream(r)
}

func applyDownloadHeaders(target Context, contentType string, opts StreamOptions) {
	if target == nil {
		return
	}

	target.SetHeader(HeaderContentType, contentType)
	if opts.Filename != "" {
		target.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", opts.Filename))
	}
	if opts.ExportID != "" {
		target.SetHeader("X-Export-Id", opts.ExportID)
	}
	if opts.ContentLength > 0 {
		target.SetHeader("Content-Length", fmt.Sprintf("%d", opts.ContentLength))
	}
}
