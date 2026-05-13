package router

import (
	"context"
	"fmt"
	"io"
	"maps"
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
	NextCalled       bool
	HeadersM         map[string]string
	CookiesM         map[string]string
	ParamsM          map[string]string
	QueriesM         map[string]string
	LocalsMock       map[any]any
	StatusCodeM      int
	ResponseBodyM    string
	ResponseHeadersM http.Header
	WrittenM         bool
	BodySizeM        int64
	StreamM          bool
	RenderBodyM      string
	RenderBytesM     []byte
}

func NewMockContext() *MockContext {
	return &MockContext{
		HeadersM:         make(map[string]string),
		CookiesM:         make(map[string]string),
		ParamsM:          make(map[string]string),
		QueriesM:         make(map[string]string),
		LocalsMock:       make(map[any]any),
		StatusCodeM:      http.StatusOK,
		ResponseHeadersM: make(http.Header),
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
	if m.hasExpectedCall("Status") {
		m.Called(code)
	}
	m.StatusCodeM = code
	return m
}

func (m *MockContext) setResponseBody(body string) {
	m.ResponseBodyM = body
	m.BodySizeM = int64(len(body))
	m.WrittenM = true
	m.StreamM = false
}

func (m *MockContext) clearResponseBody() {
	m.setResponseBody("")
}

func (m *MockContext) SendString(s string) error {
	var err error
	if m.hasExpectedCall("SendString") {
		args := m.Called(s)
		err = args.Error(0)
	}
	m.setResponseBody(s)
	return err
}

func (m *MockContext) Send(b []byte) error {
	var err error
	if m.hasExpectedCall("Send") {
		args := m.Called(b)
		err = args.Error(0)
	}
	m.setResponseBody(string(b))
	return err
}

func (m *MockContext) SendStream(r io.Reader) error {
	if m.hasExpectedCall("SendStream") {
		args := m.Called(r)
		if err := args.Error(0); err != nil {
			return err
		}
	}
	m.ResponseBodyM = ""
	m.BodySizeM = 0
	m.WrittenM = true
	m.StreamM = true
	return nil
}

func (m *MockContext) JSON(code int, val any) error {
	var err error
	if m.hasExpectedCall("JSON") {
		args := m.Called(code, val)
		err = args.Error(0)
	}
	m.StatusCodeM = code
	m.setResponseBody(fmt.Sprintf("%v", val))
	return err
}

func (m *MockContext) NoContent(code int) error {
	var err error
	if m.hasExpectedCall("NoContent") {
		args := m.Called(code)
		err = args.Error(0)
	}
	m.StatusCodeM = code
	m.clearResponseBody()
	return err
}

func (m *MockContext) Render(name string, bind any, layout ...string) error {
	var err error
	if len(layout) > 0 && m.hasExpectedCall("Render") {
		args := m.Called(name, bind, layout[0])
		err = args.Error(0)
	} else if m.hasExpectedCall("Render") {
		args := m.Called(name, bind)
		err = args.Error(0)
	}
	if err != nil {
		return err
	}
	if m.RenderBytesM != nil {
		m.ResponseBodyM = string(m.RenderBytesM)
	} else if m.RenderBodyM != "" {
		m.ResponseBodyM = m.RenderBodyM
	} else {
		m.ResponseBodyM = fmt.Sprintf("rendered: %s", name)
	}
	m.setResponseBody(m.ResponseBodyM)
	return nil
}

func (m *MockContext) RenderToWriter(w io.Writer, name string, bind any, layouts ...string) error {
	if w == nil {
		return fmt.Errorf("render: writer is nil")
	}

	if m.hasExpectedCall("RenderToWriter") {
		args := []any{name, bind}
		for _, layout := range layouts {
			args = append(args, layout)
		}
		called := m.Called(args...)
		return called.Error(0)
	}

	b, err := m.RenderToBytes(name, bind, layouts...)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func (m *MockContext) RenderToBytes(name string, bind any, layouts ...string) ([]byte, error) {
	if m.hasExpectedCall("RenderToBytes") {
		args := []any{name, bind}
		for _, layout := range layouts {
			args = append(args, layout)
		}
		called := m.Called(args...)
		if b, ok := called.Get(0).([]byte); ok {
			return b, called.Error(1)
		}
		if s, ok := called.Get(0).(string); ok {
			return []byte(s), called.Error(1)
		}
		return nil, called.Error(1)
	}

	if m.RenderBytesM != nil {
		out := make([]byte, len(m.RenderBytesM))
		copy(out, m.RenderBytesM)
		return out, nil
	}

	body := m.RenderBodyM
	if body == "" {
		body = fmt.Sprintf("rendered: %s", name)
	}
	return []byte(body), nil
}

func (m *MockContext) hasExpectedCall(method string) bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == method {
			return true
		}
	}
	return false
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
	if m.hasExpectedCall("SetHeader") {
		m.Called(key, val)
	}
	if m.ResponseHeadersM == nil {
		m.ResponseHeadersM = make(http.Header)
	}
	m.ResponseHeadersM.Set(key, val)
	return m
}

func (m *MockContext) AppendResponseHeader(key, val string) Context {
	if m.hasExpectedCall("AppendResponseHeader") {
		m.Called(key, val)
	}
	if m.ResponseHeadersM == nil {
		m.ResponseHeadersM = make(http.Header)
	}
	m.ResponseHeadersM.Add(key, val)
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
	var err error
	if m.hasExpectedCall("SendStatus") {
		args := m.Called(code)
		err = args.Error(0)
	}
	m.StatusCodeM = code
	m.clearResponseBody()
	return err
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
	if cookie == nil {
		return
	}
	if m.hasExpectedCall("Cookie") {
		m.Called(cookie)
	}
	if m.ResponseHeadersM == nil {
		m.ResponseHeadersM = make(http.Header)
	}
	if stdCookie := routerCookieToHTTP(cookie); stdCookie != nil {
		m.ResponseHeadersM.Add("Set-Cookie", stdCookie.String())
	}
	if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(time.Now())) {
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

func (m *MockContext) QueryValues(key string) []string {
	args := m.Called(key)
	if args.Get(0) == nil {
		return []string{}
	}
	return args.Get(0).([]string)
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
		maps.Copy(merged, existingMap)
		maps.Copy(merged, value)
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

func (m *MockContext) StatusCode() int {
	if m == nil || m.StatusCodeM <= 0 {
		return http.StatusOK
	}
	return m.StatusCodeM
}

func (m *MockContext) ResponseHeaders() http.Header {
	if m == nil || m.ResponseHeadersM == nil {
		return http.Header{}
	}
	return cloneHTTPHeader(m.ResponseHeadersM)
}

func (m *MockContext) ResponseWritten() bool {
	return m != nil && m.WrittenM
}

func (m *MockContext) ResponseBodySize() int64 {
	if m == nil {
		return 0
	}
	return m.BodySizeM
}

func (m *MockContext) ResponseIsStream() bool {
	return m != nil && m.StreamM
}
