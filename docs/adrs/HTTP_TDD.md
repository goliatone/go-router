HTTP_TDD: net/http Handler Adapter for go-router
================================================

Status: draft
Owner: go-router
Audience: router maintainers and component authors

Goal
----
Enable reuse of standard net/http handlers inside go-router across all adapters
(HTTPRouter and Fiber) without leaking adapter-specific types. This allows
components (ex: timezones in go-formgen) to ship a single http.Handler and use
the same logic in go-router.

Non-goals
---------
- Do not redesign the go-router Context API.
- Do not require net/http support in adapters that cannot expose it.
- Do not implement advanced http.ResponseWriter interfaces (Hijacker, Pusher)
  unless explicitly needed later.

Background
----------
go-router handlers are defined as:
  type HandlerFunc func(Context) error
and Context abstracts HTTP behavior for Fiber and HTTPRouter adapters.
Currently there is no adapter that converts net/http handlers into HandlerFunc.
There is a middleware adapter (MiddlewareFromHTTP), but no handler adapter.

We need:
- A small, adapter-agnostic interface to expose net/http primitives.
- A HandlerFromHTTP adapter that uses that interface.
- Adapter-specific implementations for HTTPRouter and Fiber.

Design Summary
--------------
1) Add a small interface in router.go:
   type HTTPContext interface {
     Request() *http.Request
     Response() http.ResponseWriter
   }

2) Add a new adapter function in router.go:
   func HandlerFromHTTP(h http.Handler) HandlerFunc

3) Implement HTTPContext on:
   - httpRouterContext (HTTPRouter adapter): direct access to *http.Request and
     http.ResponseWriter.
   - fiberContext (Fiber adapter): convert fasthttp.RequestCtx into net/http
     types via fasthttpadaptor and a small http.ResponseWriter shim.

Proposed API
------------
// router.go
type HTTPContext interface {
  Request() *http.Request
  Response() http.ResponseWriter
}

// router.go
// HandlerFromHTTP adapts a net/http handler to a go-router HandlerFunc.
// Works with any Context that also implements HTTPContext.
func HandlerFromHTTP(h http.Handler) HandlerFunc

HandlerFromHTTP Behavior
------------------------
- If h is nil: return a go-errors error (500).
- If Context does not implement HTTPContext: return a go-errors error (501).
- If Request() or Response() are nil: return a go-errors error (500).
- Call h.ServeHTTP(response, request) and return nil.
- Do not call c.Next(). This matches the net/http handler contract.
- Ensure request context is preserved: set req = req.WithContext(c.Context())
  before calling h.ServeHTTP.

HTTPRouter Adapter Implementation
---------------------------------
Add methods to httpRouterContext in httprouter.go:
  func (c *httpRouterContext) Request() *http.Request { return c.r }
  func (c *httpRouterContext) Response() http.ResponseWriter { return c.w }

This adapter already stores the net/http types, so no conversion is needed.

Fiber Adapter Implementation
----------------------------
Fiber uses fasthttp. We must convert to net/http types.

Dependencies:
- github.com/valyala/fasthttp
- github.com/valyala/fasthttp/fasthttpadaptor
Fiber already depends on fasthttp, but the adaptor is a new import.

Add cached fields to fiberContext:
  httpReq *http.Request
  httpRes http.ResponseWriter

Implement HTTPContext on fiberContext:
  func (c *fiberContext) Request() *http.Request
  func (c *fiberContext) Response() http.ResponseWriter

Implementation sketch:
  func (c *fiberContext) Request() *http.Request {
    ctx := c.liveCtx()
    if ctx == nil { return nil }
    if c.httpReq != nil { return c.httpReq }
    req := &http.Request{}
    // Convert request including body so handlers can read r.Body.
    if err := fasthttpadaptor.ConvertRequest(ctx.Context(), req, false); err != nil {
      return nil
    }
    req = req.WithContext(c.Context())
    c.httpReq = req
    return req
  }

  func (c *fiberContext) Response() http.ResponseWriter {
    ctx := c.liveCtx()
    if ctx == nil { return nil }
    if c.httpRes != nil { return c.httpRes }
    c.httpRes = &fasthttpResponseWriter{ctx: ctx.Context()}
    return c.httpRes
  }

ResponseWriter shim (minimal):
  type fasthttpResponseWriter struct {
    ctx         *fasthttp.RequestCtx
    header      http.Header
    wroteHeader bool
    wroteBody   bool
  }

  func (w *fasthttpResponseWriter) Header() http.Header {
    if w.header == nil { w.header = make(http.Header) }
    return w.header
  }

  func (w *fasthttpResponseWriter) WriteHeader(status int) {
    if w.wroteHeader { return }
    w.wroteHeader = true
    for k, vals := range w.header {
      for _, v := range vals {
        w.ctx.Response.Header.Add(k, v)
      }
    }
    w.ctx.Response.SetStatusCode(status)
  }

  func (w *fasthttpResponseWriter) Write(p []byte) (int, error) {
    if !w.wroteHeader { w.WriteHeader(http.StatusOK) }
    w.wroteBody = true
    return w.ctx.Write(p)
  }

  // Finalize ensures header-only responses are flushed.
  func (w *fasthttpResponseWriter) Finalize() {
    if !w.wroteHeader {
      w.WriteHeader(http.StatusOK)
    }
  }

Notes for Fiber:
- liveCtx() already guards against websocket hijack; if liveCtx() is nil,
  Request/Response return nil.
- The shim does not implement Flusher, Hijacker, Pusher. This is fine for
  JSON and typical request/response handlers (like timezones).
- For HEAD requests, wrap the ResponseWriter so body writes are discarded
  while preserving headers/status.

Usage Example
-------------
// Example for timezones handler (net/http)
tzHandler := timezones.Handler()

app.Router().Get("/api/timezones", router.HandlerFromHTTP(tzHandler))
app.Router().Head("/api/timezones", router.HandlerFromHTTP(tzHandler))

Notes:
- go-router treats HEAD as a separate route, so register HEAD explicitly if
  the handler should support it.

Error Handling
--------------
HandlerFromHTTP returns an error when:
- Context does not implement HTTPContext
- Request or Response is nil
- h is nil

HandlerFromHTTP returns go-errors errors with HTTP status codes so the default
error handlers can serialize them consistently.

Default HTTPRouter adapter behavior logs and writes 500 when a handler returns
an error. If a custom error middleware is installed, it can translate errors
to JSON error responses.

Compatibility and Constraints
------------------------------
- HTTPContext is optional; existing handlers remain unchanged.
- Only adapters that can expose net/http primitives should implement it.
- Fiber conversion is best-effort and should be used for standard HTTP flows.
- Not intended for streaming or websocket upgrade handlers.

Testing Plan
------------
New tests in go-router:
1) HandlerFromHTTP with HTTPRouter adapter:
   - Register a net/http handler that sets headers and JSON body.
   - Assert status, headers, and body on response.
   - Register GET and HEAD; HEAD returns empty body.

2) HandlerFromHTTP with Fiber adapter:
   - Register a net/http handler that reads query params and writes a header.
   - Use fiber test client to assert status, header, body.
   - Register a handler that only sets headers and writes no body; verify
     headers are flushed.
   - Register a handler that reads r.Body; ensure body conversion works.
   - Register HEAD and ensure body is suppressed while status/headers remain.

3) Negative tests:
   - Use a mock Context that does not implement HTTPContext; expect error.
   - Use a Context that returns nil Request or Response; expect error.
   - Pass a nil http.Handler; expect error.

Acceptance Criteria
-------------------
- HandlerFromHTTP compiles and is usable from go-router.
- HTTPRouter adapter exposes Request/Response directly.
- Fiber adapter converts request/response correctly for standard handlers.
- Tests cover success cases for both adapters and error cases for unsupported
  contexts.

Implementation Tasks
--------------------
1) Add HTTPContext interface to router.go (new type).
2) Add HandlerFromHTTP to router.go (new function).
3) Implement HTTPContext on httpRouterContext (httprouter.go).
4) Implement HTTPContext on fiberContext (fiber.go).
5) Add fasthttpadaptor import and response writer shim for Fiber.
6) Add tests for HTTPRouter and Fiber.
7) Update README with usage example (optional but recommended).

Open Questions
--------------
- Should fasthttpResponseWriter implement http.Flusher?
- Should we expose an opt-in interface for adapters to provide a real
  http.ResponseWriter instead of the shim?
