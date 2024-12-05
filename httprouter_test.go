package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			middlewareCalled = true
			next.ServeHTTP(w, req)
		})
	}

	handler := func(ctx Context) error {
		return ctx.Send([]byte("Hello with Middleware!"))
	}

	router.Use(MiddlewareFromHTTP(middleware))
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

func CreateContextMiddleware(key, value string) HandlerFunc {
	return func(c Context) error {
		newCtx := context.WithValue(c.Context(), key, value)
		c.SetContext(newCtx)
		return c.Next()
	}
}

func TestHTTPRouter_ContextPropagation2(t *testing.T) {
	adapter := NewHTTPServer()
	router := adapter.Router()

	// Create middleware using the helper function
	middleware := CreateContextMiddleware("mykey", "myvalue")
	router.Use(ToMiddleware(middleware))

	router.Get("/test", func(c Context) error {
		val := c.Context().Value("mykey")
		if val != "myvalue" {
			return fmt.Errorf("expected context value 'myvalue', got '%v'", val)
		}
		return nil
	})

	// Test the endpoint
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	adapter.WrappedRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %v", w.Code)
	}
}

func TestHTTPRouter_ContextPropagation(t *testing.T) {
	adapter := NewHTTPServer()
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

	middleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "middleware1")
			next.ServeHTTP(w, req)
		})
	}

	middleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "middleware2")
			next.ServeHTTP(w, req)
		})
	}

	handler := func(ctx Context) error {
		order = append(order, "handler")
		return ctx.Send([]byte("Middleware Chain OK"))
	}

	router.Use(MiddlewareFromHTTP(middleware1))
	router.Use(MiddlewareFromHTTP(middleware2))
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
	if len(order) != len(expectedOrder) {
		t.Errorf("Expected order %v, got %v", expectedOrder, order)
	} else {
		for i := range order {
			if order[i] != expectedOrder[i] {
				t.Errorf("At index %d, expected '%s', got '%s'", i, expectedOrder[i], order[i])
			}
		}
	}
}
