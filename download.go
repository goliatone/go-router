package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
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

// FilenameSanitizer normalizes a filename for download headers.
type FilenameSanitizer func(string) string

// StreamOptions configures download stream responses.
type StreamOptions struct {
	Filename          string
	ExportID          string
	ContentLength     int64
	MaxBufferBytes    int64
	FilenameSanitizer FilenameSanitizer
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

// WithFilenameSanitizer overrides the default filename sanitizer.
func WithFilenameSanitizer(sanitizer FilenameSanitizer) StreamOption {
	return func(opts *StreamOptions) {
		opts.FilenameSanitizer = sanitizer
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
	if payload.Bytes != nil {
		reader = bytes.NewReader(payload.Bytes)
		size = int64(len(payload.Bytes))
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
	return sendStream(target, r, options)
}

func applyDownloadHeaders(target Context, contentType string, opts StreamOptions) {
	if target == nil {
		return
	}

	target.SetHeader(HeaderContentType, contentType)
	if opts.Filename != "" {
		filename := sanitizeFilename(opts.Filename, opts.FilenameSanitizer)
		if filename != "" {
			if value := formatContentDisposition(filename); value != "" {
				target.SetHeader("Content-Disposition", value)
			}
		}
	}
	if opts.ExportID != "" {
		target.SetHeader("X-Export-Id", opts.ExportID)
	}
	if opts.ContentLength > 0 {
		target.SetHeader("Content-Length", fmt.Sprintf("%d", opts.ContentLength))
	}
}

func sanitizeFilename(filename string, sanitizer FilenameSanitizer) string {
	if sanitizer != nil {
		return sanitizer(filename)
	}
	return defaultFilenameSanitizer(filename)
}

func defaultFilenameSanitizer(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = path.Base(filename)
	filename = strings.TrimSpace(filename)
	if filename == "." || filename == ".." {
		return ""
	}
	filename = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return -1
		case r == '/' || r == '\\' || r == '"':
			return -1
		}
		return r
	}, filename)
	filename = strings.TrimSpace(filename)
	if filename == "." || filename == ".." {
		return ""
	}
	return filename
}

func formatContentDisposition(filename string) string {
	if filename == "" {
		return ""
	}
	value := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if value != "" {
		return value
	}
	filename = strings.ReplaceAll(filename, "\"", "")
	if filename == "" {
		return ""
	}
	return fmt.Sprintf("attachment; filename=\"%s\"", filename)
}

func sendStream(target Context, r io.Reader, opts StreamOptions) error {
	if opts.MaxBufferBytes <= 0 {
		return target.SendStream(r)
	}
	if opts.ContentLength > 0 {
		if opts.ContentLength <= opts.MaxBufferBytes {
			return sendBuffered(target, r, opts.MaxBufferBytes)
		}
		return target.SendStream(r)
	}
	buffered, stream, err := bufferForStream(r, opts.MaxBufferBytes)
	if err != nil {
		return err
	}
	if stream != nil {
		return target.SendStream(stream)
	}
	return target.Send(buffered)
}

func sendBuffered(target Context, r io.Reader, max int64) error {
	buf, exceeded, err := readUpTo(r, max)
	if err != nil {
		return err
	}
	if exceeded {
		return target.SendStream(io.MultiReader(bytes.NewReader(buf), r))
	}
	return target.Send(buf)
}

func bufferForStream(r io.Reader, max int64) ([]byte, io.Reader, error) {
	buf, exceeded, err := readUpTo(r, max)
	if err != nil {
		return nil, nil, err
	}
	if !exceeded {
		return buf, nil, nil
	}
	return nil, io.MultiReader(bytes.NewReader(buf), r), nil
}

func readUpTo(r io.Reader, max int64) ([]byte, bool, error) {
	if max <= 0 {
		buf, err := io.ReadAll(r)
		return buf, false, err
	}
	limited := io.LimitReader(r, max+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > max {
		return buf, true, nil
	}
	return buf, false, nil
}
