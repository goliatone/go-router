package router

import "net/http"

// ResponseState is an optional context capability for inspecting response
// lifecycle state without changing request-header lookup semantics.
type ResponseState interface {
	StatusCode() int
	ResponseHeaders() http.Header
	ResponseWritten() bool
	ResponseBodySize() int64
	ResponseIsStream() bool
}

// ResponseHeaderAppender is an optional context capability for preserving
// repeated response headers without widening the main Context interface.
type ResponseHeaderAppender interface {
	AppendResponseHeader(key string, value string) Context
}

// AsResponseState returns response lifecycle inspection when supported.
func AsResponseState(c Context) (ResponseState, bool) {
	if c == nil {
		return nil, false
	}
	state, ok := c.(ResponseState)
	return state, ok
}

// AsResponseHeaderAppender returns append-style response header mutation when supported.
func AsResponseHeaderAppender(c Context) (ResponseHeaderAppender, bool) {
	if c == nil {
		return nil, false
	}
	appender, ok := c.(ResponseHeaderAppender)
	return appender, ok
}

func cloneHTTPHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for key, values := range h {
		out[key] = append([]string(nil), values...)
	}
	return out
}
