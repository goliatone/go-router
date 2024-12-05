package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestHTTPServer_Router(t *testing.T) {
	adapter := NewHTTPServer()
	if adapter == nil {
		t.Fatal("Expected adapter to be non-nil")
	}

	router := adapter.Router()
	if router == nil {
		t.Fatal("Expected router to be non-nil")
	}
}

func TestHTTPRouter_Handle(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	handler := func(ctx Context) error {
		return ctx.Send([]byte("Hello, HTTPRouter!"))
	}

	router.Handle(GET, "/hello", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/hello")
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	resp.Body.Close()

	bodyString := string(bodyBytes)
	expectedBody := "Hello, HTTPRouter!"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}
}

func TestHTTPRouter_Methods(t *testing.T) {
	adapter := NewHTTPServer()
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
	client := &http.Client{}
	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	for _, method := range methods {
		req, err := http.NewRequest(method, server.URL+"/test", nil)
		if err != nil {
			t.Fatalf("Error creating request for method %s: %v", method, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Error making request with method %s: %v", method, err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d for method %s, got %d", http.StatusOK, method, resp.StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Error reading response body for method %s: %v", method, err)
		}
		resp.Body.Close()

		bodyString := string(bodyBytes)
		expectedBody := "Method OK"

		if bodyString != expectedBody {
			t.Errorf("Expected body '%s' for method %s, got '%s'", expectedBody, method, bodyString)
		}
	}
}

func TestHTTPRouter_Group(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	apiGroup := router.Group("/api")

	handler := func(ctx Context) error {
		return ctx.Send([]byte("Hello from API Group"))
	}

	apiGroup.Get("/hello", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/hello")
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	resp.Body.Close()

	bodyString := string(bodyBytes)
	expectedBody := "Hello from API Group"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}
}

func TestHTTPRouter_ContextMethods(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	handler := func(ctx Context) error {
		if ctx.Method() != "GET" {
			t.Errorf("Expected method GET, got %s", ctx.Method())
		}
		if ctx.Path() != "/context/test/123" {
			t.Errorf("Expected path /context/test/123, got %s", ctx.Path())
		}
		id := ctx.Param("id")
		if id != "123" {
			t.Errorf("Expected param id=123, got %s", id)
		}
		q := ctx.Query("q")
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

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL+"/context/test/123?q=test", nil)
	if err != nil {
		t.Fatalf("Error creating request: %v", err)
	}
	req.Header.Set("X-Test-Header", "testvalue")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Error making request: %v", err)
	}

	if resp.StatusCode != 202 {
		t.Errorf("Expected status code 202, got %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	resp.Body.Close()

	bodyString := string(bodyBytes)
	expectedBody := "Context Test OK"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}

	responseHeader := resp.Header.Get("X-Response-Header")
	if responseHeader != "responsevalue" {
		t.Errorf("Expected response header X-Response-Header=responsevalue, got %s", responseHeader)
	}
}

func TestHTTPRouter_Bind(t *testing.T) {
	adapter := NewHTTPServer()
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

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	payload := `{"name": "HTTPRouter"}`
	resp, err := http.Post(server.URL+"/bind", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Error making POST request: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	resp.Body.Close()

	bodyString := strings.TrimSpace(string(bodyBytes))
	expectedBody := `{"name":"HTTPRouter"}`

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}
}

func TestHTTPRouter_Middleware(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	var middlewareCalled bool

	middleware := func(next HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			middlewareCalled = true
			return next(ctx)
		}
	}

	handler := func(ctx Context) error {
		return ctx.Send([]byte("Hello with Middleware!"))
	}

	router.Use(middleware)
	router.Get("/middleware", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/middleware")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	resp.Body.Close()

	bodyString := string(bodyBytes)
	expectedBody := "Hello with Middleware!"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}

	if !middlewareCalled {
		t.Errorf("Expected middleware to be called")
	}
}

func TestHTTPRouter_SetGetHeader(t *testing.T) {
	adapter := NewHTTPServer()
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

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL+"/header", nil)
	if err != nil {
		t.Fatalf("Error creating request: %v", err)
	}
	req.Header.Set("X-Test-Header", "testvalue")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	resp.Body.Close()

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

func TestHTTPRouter_ContextPropagation(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	contextMiddleware := func(next HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			newCtx := context.WithValue(ctx.Context(), "mykey", "myvalue")
			ctx.SetContext(newCtx)
			return next(ctx)
		}
	}

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

	router.Use(contextMiddleware)
	router.Get("/context", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/context")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
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
func TestHTTPRouter_MiddlewareChain(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	var order []string

	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "middleware1")
			return next(c)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "middleware2")
			return next(c)
		}
	}

	handler := func(ctx Context) error {
		order = append(order, "handler")
		return ctx.Send([]byte("Middleware Chain OK"))
	}

	router.Use(middleware1, middleware2)
	router.Get("/chain", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/chain")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	resp.Body.Close()

	bodyString := string(bodyBytes)
	expectedBody := "Middleware Chain OK"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}

	expectedOrder := []string{"middleware1", "middleware2", "handler"}
	if !reflect.DeepEqual(order, expectedOrder) {
		t.Errorf("Expected order %v, got %v", expectedOrder, order)
	}
}

func TestHTTPRouter_UseWithPriority(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	var order []string

	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "middleware1")
			return next(c)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "middleware2")
			return next(c)
		}
	}

	middleware3 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "middleware3")
			return next(c)
		}
	}

	handler := func(ctx Context) error {
		order = append(order, "handler")
		return ctx.Send([]byte("OK"))
	}

	router.UseWithPriority(1, middleware1)
	router.UseWithPriority(3, middleware2)
	router.UseWithPriority(2, middleware3)
	router.Get("/priority", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	_, err := http.Get(server.URL + "/priority")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}

	expected := []string{"middleware2", "middleware3", "middleware1", "handler"}
	if !reflect.DeepEqual(order, expected) {
		t.Errorf("Expected order %v, got %v", expected, order)
	}
}

func TestContext_GetSet(t *testing.T) {
	adapter := NewHTTPServer()
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

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	_, err := http.Get(server.URL + "/store")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}
}

func TestMiddleware_Abort(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	abortingMiddleware := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			c.Abort()
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
	}

	handler := func(c Context) error {
		t.Error("Handler should not be called after abort")
		return nil
	}

	router.Use(abortingMiddleware)
	router.Get("/abort", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/abort")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status code %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Error decoding response: %v", err)
	}

	if result["error"] != "unauthorized" {
		t.Errorf("Expected error message 'unauthorized', got %s", result["error"])
	}
}
