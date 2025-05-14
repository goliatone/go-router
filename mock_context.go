package router

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/stretchr/testify/mock"
)

// MockContext for testing providing testify mock for
// all methods, and direct field access for commonly
// used methods like Header, Param, Query, etc to make
// testing easier
type MockContext struct {
	mock.Mock
	NextCalled   bool
	headers      map[string]string
	cookies      map[string]string
	params       map[string]string
	queries      map[string]string
	locals       map[any]any
	statusCode   int
	responseBody string
}

func NewMockContext() *MockContext {
	return &MockContext{
		headers: make(map[string]string),
		cookies: make(map[string]string),
		params:  make(map[string]string),
		queries: make(map[string]string),
		locals:  make(map[any]any),
	}
}

func (m *MockContext) Next() error {
	m.NextCalled = true
	return nil
}

func (m *MockContext) Context() context.Context {
	args := m.Called()
	if args.Get(0) == nil {
		return context.Background()
	}
	return args.Get(0).(context.Context)
}

func (m *MockContext) SetContext(ctx context.Context) {
	m.Called(ctx)
}

func (m *MockContext) Path() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockContext) Method() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockContext) Body() []byte {
	args := m.Called()
	return args.Get(0).([]byte)
}

func (m *MockContext) Status(code int) Context {
	m.Called(code)
	m.statusCode = code
	return m
}

func (m *MockContext) SendString(s string) error {
	args := m.Called(s)
	m.responseBody = s
	return args.Error(0)
}

func (m *MockContext) Send(b []byte) error {
	args := m.Called(b)
	m.responseBody = string(b)
	return args.Error(0)
}

func (m *MockContext) JSON(code int, val any) error {
	args := m.Called(code, val)
	m.statusCode = code
	m.responseBody = fmt.Sprintf("%v", val)
	return args.Error(0)
}

func (m *MockContext) NoContent(code int) error {
	args := m.Called(code)
	m.statusCode = code
	return args.Error(0)
}

func (m *MockContext) Render(name string, bind any, layout ...string) error {
	if len(layout) > 0 {
		args := m.Called(name, bind, layout[0])
		return args.Error(0)
	}
	args := m.Called(name, bind)
	m.responseBody = fmt.Sprintf("rendered: %s", name)
	return args.Error(0)
}

func (m *MockContext) Redirect(path string, status ...int) error {
	if len(status) > 0 {
		args := m.Called(path, status)
		m.statusCode = status[0]
		return args.Error(0)
	}
	args := m.Called(path)
	m.statusCode = http.StatusFound
	return args.Error(0)
}

func (m *MockContext) RedirectToRoute(name string, data ViewContext, status ...int) error {
	if len(status) > 0 {
		args := m.Called(name, data, status[0])
		return args.Error(0)
	}
	args := m.Called(name, data)
	return args.Error(0)
}

func (m *MockContext) RedirectBack(fallback string, status ...int) error {
	if len(status) > 0 {
		args := m.Called(fallback, status)
		m.statusCode = status[0]
		return args.Error(0)
	}
	args := m.Called(fallback)
	m.statusCode = http.StatusFound
	return args.Error(0)
}

func (m *MockContext) SetHeader(key, val string) Context {
	m.Called(key, val)
	return m
}

func (m *MockContext) FormFile(key string) (*multipart.FileHeader, error) {
	args := m.Called(key)
	return args.Get(0).(*multipart.FileHeader), args.Error(1)
}

func (m *MockContext) FormValue(key string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		args := m.Called(key, defaultValue[0])
		return args.String(0)
	}
	args := m.Called(key)
	return args.String(0)
}

func (m *MockContext) SendStatus(code int) error {
	args := m.Called(code)
	m.statusCode = code
	return args.Error(0)
}

func (m *MockContext) Header(key string) string {
	return m.headers[key]
}

func (m *MockContext) Get(key string, defaultValue any) any {
	args := m.Called(key, defaultValue)
	return args.Get(0)
}

func (m *MockContext) GetBool(key string, defaultValue bool) bool {
	args := m.Called(key, defaultValue)
	return args.Bool(0)
}

func (m *MockContext) GetInt(key string, def int) int {
	args := m.Called(key, def)
	return args.Int(0)
}

func (m *MockContext) Set(key string, val any) {
	m.Called(key, val)
	m.locals[key] = val
}

func (m *MockContext) Bind(i any) error {
	args := m.Called(i)
	return args.Error(0)
}

func (m *MockContext) BindJSON(i any) error {
	args := m.Called(i)
	return args.Error(0)
}

func (m *MockContext) BindXML(i any) error {
	args := m.Called(i)
	return args.Error(0)
}

func (m *MockContext) BindQuery(i any) error {
	args := m.Called(i)
	return args.Error(0)
}

func (m *MockContext) CookieParser(i any) error {
	args := m.Called(i)
	return args.Error(0)
}

func (m *MockContext) Cookie(cookie *Cookie) {
	m.Called(cookie)
	if cookie.Expires.Before(time.Now()) {
		delete(m.cookies, cookie.Name)
		return
	}
	m.cookies[cookie.Name] = cookie.Value
}

func (m *MockContext) Cookies(key string, defaultValue ...string) string {
	val, ok := m.cookies[key]
	if !ok {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return val
}

func (m *MockContext) Param(key string, defaultValue ...string) string {
	val, ok := m.params[key]
	if !ok {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return val
}

func (m *MockContext) ParamsInt(key string, defaultValue int) int {
	args := m.Called(key, defaultValue)
	return args.Int(0)
}

func (m *MockContext) Query(key string, defaultValue ...string) string {
	val, ok := m.queries[key]
	if !ok {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return val
}

func (m *MockContext) QueryInt(key string, defaultValue int) int {
	args := m.Called(key, defaultValue)
	return args.Int(0)
}

func (m *MockContext) Queries() map[string]string {
	return m.queries
}

func (m *MockContext) GetString(key string, defaultValue string) string {
	args := m.Called(key, defaultValue)
	return args.String(0)
}

func (m *MockContext) Locals(key any, value ...any) any {
	if len(value) > 0 {
		m.Called(key, value[0])
		m.locals[key] = value[0]
		return nil
	}
	return m.locals[key]
}

func (m *MockContext) OriginalURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockContext) OnNext(callback func() error) {
	m.Called(callback)
}

func (m *MockContext) Referer() string {
	args := m.Called()
	return args.String(0)
}
