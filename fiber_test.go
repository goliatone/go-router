package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	router.Use(middleware)
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

func TestFiberContext(t *testing.T) {
	adapter := NewFiberAdapter()
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

	router.Use(middleware1)
	router.Use(middleware2)

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

	router.Use(contextMiddleware)

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
