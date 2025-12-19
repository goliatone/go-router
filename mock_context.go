package router

import (
	"context"
	"fmt"
	"io"
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
	NextCalled    bool
	HeadersM      map[string]string
	CookiesM      map[string]string
	ParamsM       map[string]string
	QueriesM      map[string]string
	LocalsMock    map[any]any
	StatusCodeM   int
	ResponseBodyM string
}

func NewMockContext() *MockContext {
	return &MockContext{
		HeadersM:   make(map[string]string),
		CookiesM:   make(map[string]string),
		ParamsM:    make(map[string]string),
		QueriesM:   make(map[string]string),
		LocalsMock: make(map[any]any),
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

func (m *MockContext) IP() string {
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
	m.StatusCodeM = code
	return m
}

func (m *MockContext) SendString(s string) error {
	args := m.Called(s)
	m.ResponseBodyM = s
	return args.Error(0)
}

func (m *MockContext) Send(b []byte) error {
	args := m.Called(b)
	m.ResponseBodyM = string(b)
	return args.Error(0)
}

func (m *MockContext) SendStream(r io.Reader) error {
	args := m.Called(r)
	return args.Error(0)
}

func (m *MockContext) JSON(code int, val any) error {
	args := m.Called(code, val)
	m.StatusCodeM = code
	m.ResponseBodyM = fmt.Sprintf("%v", val)
	return args.Error(0)
}

func (m *MockContext) NoContent(code int) error {
	args := m.Called(code)
	m.StatusCodeM = code
	return args.Error(0)
}

func (m *MockContext) Render(name string, bind any, layout ...string) error {
	if len(layout) > 0 {
		args := m.Called(name, bind, layout[0])
		return args.Error(0)
	}
	args := m.Called(name, bind)
	m.ResponseBodyM = fmt.Sprintf("rendered: %s", name)
	return args.Error(0)
}

func (m *MockContext) Redirect(path string, status ...int) error {
	if len(status) > 0 {
		args := m.Called(path, status)
		m.StatusCodeM = status[0]
		return args.Error(0)
	}
	args := m.Called(path)
	m.StatusCodeM = http.StatusFound
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
		m.StatusCodeM = status[0]
		return args.Error(0)
	}
	args := m.Called(fallback)
	m.StatusCodeM = http.StatusFound
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
	m.StatusCodeM = code
	return args.Error(0)
}

func (m *MockContext) Header(key string) string {
	return m.HeadersM[key]
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
	m.LocalsMock[key] = val
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
		delete(m.CookiesM, cookie.Name)
		return
	}
	m.CookiesM[cookie.Name] = cookie.Value
}

func (m *MockContext) Cookies(key string, defaultValue ...string) string {
	val, ok := m.CookiesM[key]
	if !ok {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return val
}

func (m *MockContext) Param(key string, defaultValue ...string) string {
	val, ok := m.ParamsM[key]
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
	val, ok := m.QueriesM[key]
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
	return m.QueriesM
}

func (m *MockContext) GetString(key string, defaultValue string) string {
	args := m.Called(key, defaultValue)
	return args.String(0)
}

func (m *MockContext) Locals(key any, value ...any) any {
	if len(value) > 0 {
		m.Called(key, value[0])
		m.LocalsMock[key] = value[0]
		return nil
	}
	return m.LocalsMock[key]
}

func (m *MockContext) LocalsMerge(key any, value map[string]any) map[string]any {
	m.Called(key, value)

	existing, exists := m.LocalsMock[key]
	if !exists {
		// No existing value, just store the new map
		m.LocalsMock[key] = value
		return value
	}

	// Try to convert existing value to map[string]any
	if existingMap, ok := existing.(map[string]any); ok {
		// Merge maps - new values override existing ones
		merged := make(map[string]any)
		for k, v := range existingMap {
			merged[k] = v
		}
		for k, v := range value {
			merged[k] = v
		}
		m.LocalsMock[key] = merged
		return merged
	}

	// If existing value is not a map, replace it entirely
	m.LocalsMock[key] = value
	return value
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

func (m *MockContext) RouteName() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockContext) RouteParams() map[string]string {
	args := m.Called()
	if args.Get(0) == nil {
		return make(map[string]string)
	}
	return args.Get(0).(map[string]string)
}
