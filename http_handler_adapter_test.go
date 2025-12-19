package router_test

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-router"
)

type stubContext struct {
	ctx context.Context
}

func (s *stubContext) Method() string { return "" }
func (s *stubContext) Path() string   { return "" }
func (s *stubContext) Param(name string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}
func (s *stubContext) ParamsInt(key string, defaultValue int) int { return defaultValue }
func (s *stubContext) Query(name string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}
func (s *stubContext) QueryInt(name string, defaultValue int) int { return defaultValue }
func (s *stubContext) Queries() map[string]string                 { return map[string]string{} }
func (s *stubContext) Body() []byte                               { return nil }
func (s *stubContext) Bind(v any) error                           { return nil }
func (s *stubContext) Locals(key any, value ...any) any           { return nil }
func (s *stubContext) LocalsMerge(key any, value map[string]any) map[string]any {
	return value
}
func (s *stubContext) Render(name string, bind any, layouts ...string) error { return nil }
func (s *stubContext) Cookie(cookie *router.Cookie)                          {}
func (s *stubContext) Cookies(key string, defaultValue ...string) string     { return "" }
func (s *stubContext) CookieParser(out any) error                            { return nil }
func (s *stubContext) Redirect(location string, status ...int) error         { return nil }
func (s *stubContext) RedirectToRoute(routeName string, params router.ViewContext, status ...int) error {
	return nil
}
func (s *stubContext) RedirectBack(fallback string, status ...int) error  { return nil }
func (s *stubContext) Header(key string) string                           { return "" }
func (s *stubContext) Referer() string                                    { return "" }
func (s *stubContext) OriginalURL() string                                { return "" }
func (s *stubContext) FormFile(key string) (*multipart.FileHeader, error) { return nil, nil }
func (s *stubContext) FormValue(key string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}
func (s *stubContext) IP() string { return "" }

func (s *stubContext) Status(code int) router.Context       { return s }
func (s *stubContext) Send(body []byte) error               { return nil }
func (s *stubContext) SendString(body string) error         { return nil }
func (s *stubContext) SendStatus(code int) error            { return nil }
func (s *stubContext) JSON(code int, v any) error           { return nil }
func (s *stubContext) SendStream(r io.Reader) error         { return nil }
func (s *stubContext) NoContent(code int) error             { return nil }
func (s *stubContext) SetHeader(k, v string) router.Context { return s }

func (s *stubContext) Set(key string, value any)               {}
func (s *stubContext) Get(key string, def any) any             { return def }
func (s *stubContext) GetString(key string, def string) string { return def }
func (s *stubContext) GetInt(key string, def int) int          { return def }
func (s *stubContext) GetBool(key string, def bool) bool       { return def }

func (s *stubContext) Context() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}
func (s *stubContext) SetContext(ctx context.Context) { s.ctx = ctx }
func (s *stubContext) Next() error                    { return nil }
func (s *stubContext) RouteName() string              { return "" }
func (s *stubContext) RouteParams() map[string]string { return map[string]string{} }

type httpStubContext struct {
	stubContext
}

func (s *httpStubContext) Request() *http.Request        { return nil }
func (s *httpStubContext) Response() http.ResponseWriter { return nil }

func TestHandlerFromHTTP_Errors(t *testing.T) {
	handler := router.HandlerFromHTTP(nil)
	err := handler(nil)
	assertGoErrorCode(t, err, http.StatusInternalServerError)

	handler = router.HandlerFromHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	err = handler(&stubContext{})
	assertGoErrorCode(t, err, http.StatusNotImplemented)

	err = handler(&httpStubContext{})
	assertGoErrorCode(t, err, http.StatusInternalServerError)
}

func assertGoErrorCode(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatalf("Expected error with code %d, got nil", code)
	}
	var ge *goerrors.Error
	if !errors.As(err, &ge) {
		t.Fatalf("Expected goerrors.Error, got %T", err)
	}
	if ge.Code != code {
		t.Fatalf("Expected code %d, got %d", code, ge.Code)
	}
}
