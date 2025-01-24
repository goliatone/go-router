package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestFiberAdapter_Router(t *testing.T) {
	adapter := NewFiberAdapter()
	if adapter == nil {
		t.Fatal("Expected adapter to be non-nil")
	}

	router := adapter.Router()
	if router == nil {
		t.Fatal("Expected router to be non-nil")
	}
}

func TestFiberRouter_Handle(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	handler := func(ctx Context) error {
		return ctx.Send([]byte("Hello, Fiber!"))
	}

	router.Handle(GET, "/hello", handler)

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/hello", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	bodyString := string(bodyBytes)
	expectedBody := "Hello, Fiber!"

	if bodyString != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, bodyString)
	}
}

func TestFiberRouter_UseMiddleware(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()
	var middlewareCalled bool

	middleware := func(ctx Context) error {
		middlewareCalled = true
		return ctx.Next()
	}

	handler := func(ctx Context) error {
		return ctx.Send([]byte("Hello with Middleware!"))
	}

	router.Use(ToMiddleware(middleware))
	router.Get("/middleware", handler)

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/middleware", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	bodyString := string(bodyBytes)
	expectedBody := "Hello with Middleware!"

	if bodyString != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, bodyString)
	}

	if !middlewareCalled {
		t.Errorf("Middleware was not called")
	}
}

func TestFiberRouter_Group(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	apiGroup := router.Group("/api")

	handler := func(ctx Context) error {
		return ctx.Send([]byte("Hello from API Group"))
	}

	apiGroup.Get("/hello", handler)

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/api/hello", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	bodyString := string(bodyBytes)
	expectedBody := "Hello from API Group"

	if bodyString != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, bodyString)
	}
}

func TestFiberRouter_Methods(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	handler := func(ctx Context) error {
		return ctx.Send([]byte("Method OK"))
	}

	router.Get("/test", handler)
	router.Post("/test", handler)
	router.Put("/test", handler)
	router.Delete("/test", handler)
	router.Patch("/test", handler)

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	app := adapter.WrappedRouter()

	for _, method := range methods {
		req := httptest.NewRequest(method, "/test", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Error while making request with method %s: %v", method, err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d for method %s, got %d", http.StatusOK, method, resp.StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("Error reading response body for method %s: %v", method, err)
		}

		bodyString := string(bodyBytes)
		expectedBody := "Method OK"

		if bodyString != expectedBody {
			t.Errorf("Expected body %s for method %s, got %s", expectedBody, method, bodyString)
		}
	}
}

func TestFiber_Context(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	handler := func(ctx Context) error {
		if ctx.Method() != "GET" {
			t.Errorf("Expected method GET, got %s", ctx.Method())
		}

		if ctx.Path() != "/context/test/123" {
			t.Errorf("Expected path /context/test/123, got %s", ctx.Path())
		}

		id := ctx.Param("id", "")
		if id != "123" {
			t.Errorf("Expected param id=123, got %s", id)
		}

		q := ctx.Query("q", "")
		if q != "test" {
			t.Errorf("Expected query q=test, got %s", q)
		}

		h := ctx.Header("X-Test-Header")
		if h != "testvalue" {
			t.Errorf("Expected header X-Test-Header=testvalue, got %s", h)
		}

		ctx.SetHeader("X-Response-Header", "responsevalue")

		ctx.Status(202)

		return ctx.Send([]byte("Context Test OK"))
	}

	router.Get("/context/test/:id", handler)

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/context/test/123?q=test", nil)
	req.Header.Set("X-Test-Header", "testvalue")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	if resp.StatusCode != 202 {
		t.Errorf("Expected status code 202, got %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	bodyString := string(bodyBytes)
	expectedBody := "Context Test OK"

	if bodyString != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, bodyString)
	}

	responseHeader := resp.Header.Get("X-Response-Header")
	if responseHeader != "responsevalue" {
		t.Errorf("Expected response header X-Response-Header=responsevalue, got %s", responseHeader)
	}
}

func TestFiberRouter_MiddlewareChain(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	var order []string

	middleware1 := func(ctx Context) error {
		order = append(order, "middleware1")
		return ctx.Next()
	}

	middleware2 := func(ctx Context) error {
		order = append(order, "middleware2")
		return ctx.Next()
	}

	handler := func(ctx Context) error {
		order = append(order, "handler")
		return ctx.Send([]byte("Middleware Chain OK"))
	}

	router.Use(ToMiddleware(middleware1))
	router.Use(ToMiddleware(middleware2))

	router.Get("/chain", handler)

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/chain", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	bodyString := string(bodyBytes)
	expectedBody := "Middleware Chain OK"

	if bodyString != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, bodyString)
	}

	expectedOrder := []string{"middleware1", "middleware2", "handler"}
	if len(order) != len(expectedOrder) {
		t.Errorf("Expected order %v, got %v", expectedOrder, order)
	} else {
		for i := range order {
			if order[i] != expectedOrder[i] {
				t.Errorf("At index %d, expected %s, got %s", i, expectedOrder[i], order[i])
			}
		}
	}
}

func TestFiberRouter_Bind(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	handler := func(ctx Context) error {
		var data struct {
			Name string `json:"name"`
		}
		if err := ctx.Bind(&data); err != nil {
			return err
		}
		return ctx.JSON(200, data)
	}

	router.Post("/bind", handler)

	app := adapter.WrappedRouter()

	payload := `{"name":"Fiber"}`
	req := httptest.NewRequest("POST", "/bind", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	bodyString := string(bodyBytes)
	expectedBody := `{"name":"Fiber"}`

	if bodyString != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, bodyString)
	}
}

func TestFiberContext_ContextMethods(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	contextMiddleware := func(ctx Context) error {
		newCtx := context.WithValue(ctx.Context(), "mykey", "myvalue")
		ctx.SetContext(newCtx)
		return ctx.Next()
	}

	router.Use(ToMiddleware(contextMiddleware))

	handler := func(ctx Context) error {
		value := ctx.Context().Value("mykey")
		if value != "myvalue" {
			t.Errorf("Expected context value 'myvalue', got '%v'", value)
		}

		newCtx := context.WithValue(ctx.Context(), "newkey", "newvalue")
		ctx.SetContext(newCtx)

		value = ctx.Context().Value("newkey")
		if value != "newvalue" {
			t.Errorf("Expected new context value 'newvalue', got '%v'", value)
		}

		return ctx.Send([]byte("Context Methods OK"))
	}

	router.Get("/context", handler)

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/context", nil)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	bodyString := string(bodyBytes)
	expectedBody := "Context Methods OK"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}
}

func TestFiberContext_SetGetHeader(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	handler := func(ctx Context) error {
		ctx.SetHeader("X-Response-Header", "responsevalue")

		reqHeader := ctx.Header("X-Test-Header")
		if reqHeader != "testvalue" {
			t.Errorf("Expected request header X-Test-Header=testvalue, got %s", reqHeader)
		}

		return ctx.Send([]byte("Header Test OK"))
	}

	router.Get("/header", handler)

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/header", nil)
	req.Header.Set("X-Test-Header", "testvalue")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	bodyString := string(bodyBytes)
	expectedBody := "Header Test OK"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}

	responseHeader := resp.Header.Get("X-Response-Header")
	if responseHeader != "responsevalue" {
		t.Errorf("Expected response header X-Response-Header=responsevalue, got %s", responseHeader)
	}
}

func TestFiberContext_GetSet(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	middleware := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			c.Set("key1", "value1")
			return next(c)
		}
	}

	handler := func(c Context) error {
		c.Set("key2", "value2")

		val1 := c.Get("key1", "")
		if val1 != "value1" {
			t.Errorf("Expected value1, got %v", val1)
		}

		val2 := c.Get("key2", "")
		if val2 != "value2" {
			t.Errorf("Expected value2, got %v", val2)
		}

		nonExistent := c.Get("nonexistent", nil)
		if nonExistent != nil {
			t.Errorf("Expected nil for nonexistent key, got %v", nonExistent)
		}

		return c.Send([]byte("OK"))
	}

	router.Use(middleware)
	router.Get("/store", handler)

	app := adapter.WrappedRouter()
	req := httptest.NewRequest("GET", "/store", nil)
	_, err := app.Test(req)
	if err != nil {
		t.Fatalf("Error testing request: %v", err)
	}
}

func TestFiberContext_QueryMethods(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	tests := []struct {
		name         string
		path         string
		queryString  string
		handler      func(Context) error
		expectedCode int
		expectedBody string
		validateFunc func(*testing.T, *http.Response)
	}{
		{
			name:        "Single Query Parameter",
			path:        "/query/single",
			queryString: "name=john",
			handler: func(c Context) error {
				val := c.Query("name", "")
				if val != "john" {
					t.Errorf("Expected query param 'name' to be 'john', got '%s'", val)
				}
				return c.JSON(200, map[string]string{"value": val})
			},
			expectedCode: 200,
		},
		{
			name:        "Multiple Query Parameters",
			path:        "/query/multiple",
			queryString: "name=john&age=25&city=nyc",
			handler: func(c Context) error {
				queries := c.Queries()
				expected := map[string]string{
					"name": "john",
					"age":  "25",
					"city": "nyc",
				}
				for k, v := range expected {
					if queries[k] != v {
						t.Errorf("Expected query param '%s' to be '%s', got '%s'", k, v, queries[k])
					}
				}
				return c.JSON(200, queries)
			},
			expectedCode: 200,
		},
		{
			name:        "URL Encoded Query Parameters",
			path:        "/query/encoded",
			queryString: "text=hello%20world&special=%21%40%23",
			handler: func(c Context) error {
				queries := c.Queries()
				expected := map[string]string{
					"text":    "hello world",
					"special": "!@#",
				}
				for k, v := range expected {
					if queries[k] != v {
						t.Errorf("Expected query param '%s' to be '%s', got '%s'", k, v, queries[k])
					}
				}
				return c.JSON(200, queries)
			},
			expectedCode: 200,
		},
		{
			name:        "Query Integer Parameter",
			path:        "/query/int",
			queryString: "age=25&invalid=abc",
			handler: func(c Context) error {
				age := c.QueryInt("age", 0)
				if age != 25 {
					t.Errorf("Expected QueryInt 'age' to be 25, got %d", age)
				}

				// Test with invalid integer
				defaultVal := c.QueryInt("invalid", 100)
				if defaultVal != 100 {
					t.Errorf("Expected QueryInt 'invalid' to return default 100, got %d", defaultVal)
				}

				// Test non-existent parameter
				missing := c.QueryInt("missing", 50)
				if missing != 50 {
					t.Errorf("Expected QueryInt 'missing' to return default 50, got %d", missing)
				}

				return c.JSON(200, map[string]int{"age": age})
			},
			expectedCode: 200,
		},
		{
			name:        "Empty Query Parameters",
			path:        "/query/empty",
			queryString: "",
			handler: func(c Context) error {
				queries := c.Queries()
				if len(queries) != 0 {
					t.Errorf("Expected empty queries map, got %v", queries)
				}

				// Test default values
				defStr := c.Query("missing", "default")
				if defStr != "default" {
					t.Errorf("Expected default string 'default', got '%s'", defStr)
				}

				defInt := c.QueryInt("missing", 42)
				if defInt != 42 {
					t.Errorf("Expected default int 42, got %d", defInt)
				}

				return c.JSON(200, queries)
			},
			expectedCode: 200,
		},
		{
			name:        "Query Parameters with Same Key",
			path:        "/query/duplicate",
			queryString: "tag=golang&tag=fiber",
			handler: func(c Context) error {
				// Fiber keeps the first value
				val := c.Query("tag", "")
				if val != "golang" {
					t.Errorf("Expected first tag value to be 'golang', got '%s'", val)
				}
				return c.JSON(200, map[string]string{"tag": val})
			},
			expectedCode: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router.Get(tt.path, tt.handler)

			app := adapter.WrappedRouter()
			url := tt.path
			if tt.queryString != "" {
				url += "?" + tt.queryString
			}

			req := httptest.NewRequest("GET", url, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Failed to test request: %v", err)
			}

			if resp.StatusCode != tt.expectedCode {
				t.Errorf("Expected status code %d, got %d", tt.expectedCode, resp.StatusCode)
			}

			if tt.validateFunc != nil {
				tt.validateFunc(t, resp)
			}

			resp.Body.Close()
		})
	}
}

func generateIndexedString(mask string, count int) string {
	var builder strings.Builder
	for i := 1; i <= count; i++ {
		builder.WriteString(fmt.Sprintf(mask, i))
	}
	return builder.String()
}

func TestFiberContext_QueryStressCases(t *testing.T) {
	adapter := NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New(fiber.Config{
			UnescapePath:      true,
			EnablePrintRoutes: true,
			StrictRouting:     false,
			ReadBufferSize:    1024 * 1024, // 1MB
		})
	})

	router := adapter.Router()

	// Configure Fiber for larger requests
	app := adapter.WrappedRouter()

	tests := []struct {
		name        string
		path        string
		queryString string
		handler     func(Context) error
	}{
		{
			name:        "Moderate Number of Parameters",
			path:        "/query/moderate",
			queryString: generateIndexedString("p%d=%d&", 9) + "last=value", // 10 parameters
			handler: func(c Context) error {
				queries := c.Queries()
				if len(queries) != 10 {
					t.Errorf("Expected 10 query parameters, got %d", len(queries))
				}
				return c.JSON(200, queries)
			},
		},
		{
			name:        "Moderate Length Parameter Values",
			path:        "/query/moderate-length",
			queryString: "text=" + strings.Repeat("value", 100), // 500 bytes
			handler: func(c Context) error {
				val := c.Query("text", "")
				if len(val) != 500 {
					t.Errorf("Expected value length 500, got %d", len(val))
				}
				return c.JSON(200, map[string]int{"length": len(val)})
			},
		},
		{
			name:        "Special Characters in Parameters",
			path:        "/query/special",
			queryString: "param=" + url.QueryEscape("!@#$%^&*()"),
			handler: func(c Context) error {
				val := c.Query("param", "")
				if val != "!@#$%^&*()" {
					t.Errorf("Expected special characters to be preserved, got '%s'", val)
				}
				return c.JSON(200, map[string]string{"value": val})
			},
		},
		{
			name:        "Unicode Characters in Parameters",
			path:        "/query/unicode",
			queryString: "param=" + url.QueryEscape("🚀世界"),
			handler: func(c Context) error {
				val := c.Query("param", "")
				if val != "🚀世界" {
					t.Errorf("Expected unicode characters to be preserved, got '%s'", val)
				}
				return c.JSON(200, map[string]string{"value": val})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router.Get(tt.path, tt.handler)

			url := tt.path
			if tt.queryString != "" {
				url += "?" + tt.queryString
			}

			req := httptest.NewRequest("GET", url, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Failed to test request: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			resp.Body.Close()
		})
	}
}

func TestFiberContext_QueryErrorCases(t *testing.T) {
	adapter := NewFiberAdapter()
	router := adapter.Router()

	tests := []struct {
		name        string
		path        string
		queryString string
		handler     func(Context) error
	}{
		{
			name:        "Malformed Query String",
			path:        "/query/malformed",
			queryString: "param=%invalid",
			handler: func(c Context) error {
				queries := c.Queries()
				val, exists := queries["param"]
				if !exists {
					t.Error("Expected param to exist even if malformed")
				}
				if val != "%invalid" {
					t.Errorf("Expected raw value '%%invalid', got '%s'", val)
				}
				return c.JSON(200, queries)
			},
		},
		{
			name:        "Integer Overflow",
			path:        "/query/overflow",
			queryString: "num=99999999999999999999",
			handler: func(c Context) error {
				// Should return default value for out-of-range integer
				val := c.QueryInt("num", 42)
				if val != 42 {
					t.Errorf("Expected default value 42 for overflow, got %d", val)
				}
				return c.JSON(200, map[string]int{"value": val})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router.Get(tt.path, tt.handler)

			app := adapter.WrappedRouter()
			url := tt.path
			if tt.queryString != "" {
				url += "?" + tt.queryString
			}

			req := httptest.NewRequest("GET", url, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Failed to test request: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			resp.Body.Close()
		})
	}
}
