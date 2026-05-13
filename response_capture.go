package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const DefaultMaxCapturedBodySize int64 = 1 << 20

var (
	ErrResponseCaptureStream   = errors.New("response capture: stream responses cannot be captured")
	ErrResponseCaptureTooLarge = errors.New("response capture: body exceeds maximum size")
)

type CapturedResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func CaptureResponse(base Context, maxBodySize int64, handler HandlerFunc) (*CapturedResponse, error) {
	if base == nil {
		return nil, fmt.Errorf("response capture: context is nil")
	}
	if handler == nil {
		return nil, fmt.Errorf("response capture: handler is nil")
	}
	capture := newResponseCaptureContext(base, maxBodySize)
	if err := handler(capture); err != nil {
		return nil, err
	}
	return capture.CapturedResponse()
}

func ReplayCapturedResponse(ctx Context, captured *CapturedResponse) error {
	if ctx == nil || captured == nil {
		return nil
	}
	appender, supportsAppend := AsResponseHeaderAppender(ctx)
	for key, values := range captured.Headers {
		if len(values) == 0 {
			continue
		}
		if len(values) == 1 || !supportsAppend {
			ctx.SetHeader(key, values[len(values)-1])
			continue
		}
		for _, value := range values {
			appender.AppendResponseHeader(key, value)
		}
	}

	code := captured.StatusCode
	if code <= 0 {
		code = http.StatusOK
	}

	if isNoBodyStatus(code) {
		return ctx.NoContent(code)
	}

	ctx.Status(code)
	return ctx.Send(append([]byte{}, captured.Body...))
}

type responseCaptureContext struct {
	contextDelegate
	statusCode int
	headers    http.Header
	body       bytes.Buffer
	maxBody    int64
	written    bool
	stream     bool
}

func newResponseCaptureContext(base Context, maxBodySize int64) *responseCaptureContext {
	if maxBodySize <= 0 {
		maxBodySize = DefaultMaxCapturedBodySize
	}
	return &responseCaptureContext{
		contextDelegate: contextDelegate{Context: base},
		statusCode:      http.StatusOK,
		headers:         make(http.Header),
		maxBody:         maxBodySize,
	}
}

type contextDelegate struct {
	Context
}

func (c *responseCaptureContext) Context() context.Context {
	return c.contextDelegate.Context.Context()
}

func (c *responseCaptureContext) Status(code int) Context {
	if code > 0 {
		c.statusCode = code
	}
	return c
}

func (c *responseCaptureContext) SetHeader(key string, value string) Context {
	c.headers.Set(key, value)
	return c
}

func (c *responseCaptureContext) AppendResponseHeader(key string, value string) Context {
	c.headers.Add(key, value)
	return c
}

func (c *responseCaptureContext) Cookie(cookie *Cookie) {
	stdCookie := routerCookieToHTTP(cookie)
	if stdCookie == nil {
		return
	}
	c.headers.Add("Set-Cookie", stdCookie.String())
}

func (c *responseCaptureContext) Send(body []byte) error {
	if body == nil {
		return c.NoContent(http.StatusNoContent)
	}
	return c.writeCapturedBody(body)
}

func (c *responseCaptureContext) SendString(body string) error {
	return c.Send([]byte(body))
}

func (c *responseCaptureContext) SendStatus(code int) error {
	c.statusCode = code
	if c.body.Len() == 0 {
		message := http.StatusText(code)
		if message == "" {
			message = fmt.Sprintf("%d", code)
		}
		return c.writeCapturedBody([]byte(message))
	}
	c.written = true
	return nil
}

func (c *responseCaptureContext) JSON(code int, v any) error {
	c.statusCode = code
	c.headers.Set(HeaderContentType, "application/json")
	if writer, ok := c.contextDelegate.Context.(responseJSONWriter); ok {
		if err := writer.encodeResponseJSONToWriter(c, v); err != nil {
			return err
		}
		c.written = true
		return nil
	}
	encoder, ok := c.contextDelegate.Context.(responseJSONEncoder)
	if !ok {
		if err := (httpRouterResponseJSONEncoder{}).encodeResponseJSONToWriter(c, v); err != nil {
			return err
		}
		c.written = true
		return nil
	}
	body, err := encoder.encodeResponseJSON(v)
	if err != nil {
		return err
	}
	if body == nil {
		c.written = true
		return nil
	}
	return c.writeCapturedBody(body)
}

type responseJSONEncoder interface {
	encodeResponseJSON(v any) ([]byte, error)
}

type responseJSONWriter interface {
	encodeResponseJSONToWriter(w io.Writer, v any) error
}

type httpRouterResponseJSONEncoder struct{}

func (httpRouterResponseJSONEncoder) encodeResponseJSON(v any) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := (httpRouterResponseJSONEncoder{}).encodeResponseJSONToWriter(buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (httpRouterResponseJSONEncoder) encodeResponseJSONToWriter(w io.Writer, v any) error {
	if v == nil {
		return nil
	}
	return json.NewEncoder(w).Encode(v)
}

func (c *responseCaptureContext) Render(name string, bind any, layouts ...string) error {
	renderer, ok := AsTemplateRenderer(c.contextDelegate.Context)
	if !ok {
		return fmt.Errorf("response capture: context does not support template rendering")
	}
	if err := renderer.RenderToWriter(c, name, bind, layouts...); err != nil {
		return err
	}
	c.headers.Set(HeaderContentType, "text/html; charset=utf-8")
	return nil
}

func (c *responseCaptureContext) NoContent(code int) error {
	c.statusCode = code
	c.written = true
	c.body.Reset()
	return nil
}

func (c *responseCaptureContext) SendStream(_ io.Reader) error {
	c.stream = true
	return ErrResponseCaptureStream
}

func (c *responseCaptureContext) Redirect(location string, status ...int) error {
	code := http.StatusFound
	if len(status) > 0 {
		code = status[0]
	}
	c.statusCode = code
	c.headers.Set("Location", location)
	c.written = true
	return nil
}

func (c *responseCaptureContext) RedirectBack(fallback string, status ...int) error {
	return c.Redirect(resolveRedirectBackTarget(c, fallback), status...)
}

func (c *responseCaptureContext) RedirectToRoute(routeName string, params ViewContext, status ...int) error {
	return fmt.Errorf("response capture: RedirectToRoute is not supported for route %q", routeName)
}

func (c *responseCaptureContext) CapturedResponse() (*CapturedResponse, error) {
	if c.stream {
		return nil, ErrResponseCaptureStream
	}
	return &CapturedResponse{
		StatusCode: c.statusCode,
		Headers:    cloneHTTPHeader(c.headers),
		Body:       append([]byte(nil), c.body.Bytes()...),
	}, nil
}

func (c *responseCaptureContext) writeCapturedBody(body []byte) error {
	if int64(c.body.Len()+len(body)) > c.maxBody {
		return ErrResponseCaptureTooLarge
	}
	if _, err := c.body.Write(body); err != nil {
		return err
	}
	c.written = true
	return nil
}

func (c *responseCaptureContext) Write(body []byte) (int, error) {
	if err := c.writeCapturedBody(body); err != nil {
		return 0, err
	}
	return len(body), nil
}

func isNoBodyStatus(code int) bool {
	return code == http.StatusNoContent || code == http.StatusResetContent || code == http.StatusNotModified
}

var _ Context = (*responseCaptureContext)(nil)
