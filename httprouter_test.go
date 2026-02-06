package router_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/goliatone/go-router"
)

func TestHTTPServer_Router(t *testing.T) {
	adapter := router.NewHTTPServer()
	if adapter == nil {
		t.Fatal("Expected adapter to be non-nil")
	}

	router := adapter.Router()
	if router == nil {
		t.Fatal("Expected router to be non-nil")
	}
}

func TestHTTPRouter_Handle(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	handler := func(ctx router.Context) error {
		return ctx.Send([]byte("Hello, HTTPRouter!"))
	}

	r.Handle(router.GET, "/hello", handler)

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
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	handler := func(ctx router.Context) error {
		return ctx.Send([]byte("Method OK"))
	}

	r.Get("/test", handler)
	r.Post("/test", handler)
	r.Put("/test", handler)
	r.Delete("/test", handler)
	r.Patch("/test", handler)

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

func TestHTTPRouter_ConflictPolicy_LogAndSkip(t *testing.T) {
	adapter := router.NewHTTPServer(router.WithHTTPRouterConflictPolicy(router.HTTPRouterConflictLogAndSkip))
	r := adapter.Router()

	handler := func(ctx router.Context) error {
		return ctx.Send([]byte("ok"))
	}

	r.Get("/admin/exports/:id", handler)
	r.Get("/admin/exports/history", handler)

	routes := r.Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route after conflict skip, got %d", len(routes))
	}
	if routes[0].Path != "/admin/exports/:id" {
		t.Fatalf("unexpected route path: %s", routes[0].Path)
	}
}

func TestHTTPRouter_Group(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	apiGroup := r.Group("/api")

	handler := func(ctx router.Context) error {
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
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	handler := func(ctx router.Context) error {
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
		return ctx.Send([]byte("router.Context Test OK"))
	}

	r.Get("/context/test/:id", handler)

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
	expectedBody := "router.Context Test OK"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}

	responseHeader := resp.Header.Get("X-Response-Header")
	if responseHeader != "responsevalue" {
		t.Errorf("Expected response header X-Response-Header=responsevalue, got %s", responseHeader)
	}
}

func TestHTTPRouter_QueryValues(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	handler := func(ctx router.Context) error {
		values := ctx.QueryValues("include")
		expected := []string{"a", "b", "c", "b"}
		if !reflect.DeepEqual(values, expected) {
			t.Errorf("Expected values %v, got %v", expected, values)
		}
		return ctx.JSON(200, map[string]any{"include": values})
	}

	r.Get("/query/values", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/query/values?include=a&include=b&include=c&include=b")
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}
}

func TestHTTPRouter_Bind(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	handler := func(ctx router.Context) error {
		var data struct {
			Name string `json:"name"`
		}
		if err := ctx.Bind(&data); err != nil {
			return err
		}
		return ctx.JSON(200, data)
	}

	r.Post("/bind", handler)

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
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	var middlewareCalled bool

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			middlewareCalled = true
			next.ServeHTTP(w, req)
		})
	}

	handler := func(ctx router.Context) error {
		return ctx.Send([]byte("Hello with Middleware!"))
	}

	r.Use(router.MiddlewareFromHTTP(middleware))
	r.Get("/middleware", handler)

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
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	handler := func(ctx router.Context) error {
		ctx.SetHeader("X-Response-Header", "responsevalue")
		reqHeader := ctx.Header("X-Test-Header")
		if reqHeader != "testvalue" {
			t.Errorf("Expected request header X-Test-Header=testvalue, got %s", reqHeader)
		}
		return ctx.Send([]byte("Header Test OK"))
	}

	r.Get("/header", handler)

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

func CreateContextMiddleware(key, value string) router.HandlerFunc {
	return func(c router.Context) error {
		newCtx := context.WithValue(c.Context(), key, value)
		c.SetContext(newCtx)
		return c.Next()
	}
}

func TestHTTPRouter_ContextPropagation2(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	// Create middleware using the helper function
	middleware := CreateContextMiddleware("mykey", "myvalue")
	r.Use(router.ToMiddleware(middleware))

	r.Get("/test", func(c router.Context) error {
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
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	contextMiddleware := func(ctx router.Context) error {
		newCtx := context.WithValue(ctx.Context(), "mykey", "myvalue")
		ctx.SetContext(newCtx)
		return ctx.Next()
	}

	r.Use(router.ToMiddleware(contextMiddleware))

	handler := func(ctx router.Context) error {
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
		return ctx.Send([]byte("router.Context Methods OK"))
	}

	r.Get("/context", handler)

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
	expectedBody := "router.Context Methods OK"

	if bodyString != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, bodyString)
	}
}

func TestHTTPRouter_MiddlewareChain(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

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

	handler := func(ctx router.Context) error {
		order = append(order, "handler")
		return ctx.Send([]byte("Middleware Chain OK"))
	}

	r.Use(router.MiddlewareFromHTTP(middleware1))
	r.Use(router.MiddlewareFromHTTP(middleware2))
	r.Get("/chain", handler)

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

func TestHTTPRouter_MiddlewareFromHTTP_WithNextChain(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	var order []string

	httpMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "http")
			next.ServeHTTP(w, req)
		})
	}

	routerMiddleware := router.ToMiddleware(func(ctx router.Context) error {
		order = append(order, "router")
		return nil
	})

	handler := func(ctx router.Context) error {
		order = append(order, "handler")
		return ctx.Send([]byte("OK"))
	}

	r.Use(router.MiddlewareFromHTTP(httpMiddleware))
	r.Use(routerMiddleware)
	r.Get("/middleware-next", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/middleware-next")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	expectedOrder := []string{"http", "router", "handler"}
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

func TestContext_GetSet(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	middleware := func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			c.Set("key1", "value1")
			return next(c)
		}
	}

	handler := func(c router.Context) error {
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

	r.Use(middleware)
	r.Get("/store", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	_, err := http.Get(server.URL + "/store")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}
}

func TestHTTPRouter_LocalsMerge(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	handler := func(ctx router.Context) error {
		// Test LocalsMerge with no existing value
		result1 := ctx.LocalsMerge("session", map[string]any{
			"user_id": 123,
			"role":    "admin",
		})

		expected1 := map[string]any{
			"user_id": 123,
			"role":    "admin",
		}

		if len(result1) != 2 || result1["user_id"] != 123 || result1["role"] != "admin" {
			t.Errorf("Expected %v, got %v", expected1, result1)
		}

		// Test LocalsMerge with existing map - should merge
		result2 := ctx.LocalsMerge("session", map[string]any{
			"last_login": "2023-01-01",
			"role":       "superuser", // Should override
		})

		expected2 := map[string]any{
			"user_id":    123,
			"role":       "superuser", // Overridden value
			"last_login": "2023-01-01",
		}

		if len(result2) != 3 || result2["user_id"] != 123 || result2["role"] != "superuser" || result2["last_login"] != "2023-01-01" {
			t.Errorf("Expected %v, got %v", expected2, result2)
		}

		// Verify the stored value is also merged
		stored := ctx.Locals("session")
		storedMap, ok := stored.(map[string]any)
		if !ok {
			t.Errorf("Expected stored value to be map[string]any, got %T", stored)
		}

		if len(storedMap) != 3 || storedMap["user_id"] != 123 || storedMap["role"] != "superuser" || storedMap["last_login"] != "2023-01-01" {
			t.Errorf("Expected stored value %v, got %v", expected2, storedMap)
		}

		// Test LocalsMerge with non-map existing value - should replace
		ctx.Locals("config", "some string value")
		result3 := ctx.LocalsMerge("config", map[string]any{
			"debug":   true,
			"timeout": 30,
		})

		expected3 := map[string]any{
			"debug":   true,
			"timeout": 30,
		}

		if len(result3) != 2 || result3["debug"] != true || result3["timeout"] != 30 {
			t.Errorf("Expected %v, got %v", expected3, result3)
		}

		return ctx.JSON(200, map[string]any{"status": "ok"})
	}

	r.Get("/locals-merge", handler)

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/locals-merge")
	if err != nil {
		t.Fatalf("Error making GET request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}
}

func TestHTTPRouter_HandlerFromHTTP(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := router.RouteNameFromContext(r.Context()); !ok || name != "http.handler" {
			t.Errorf("Expected route name http.handler, got %v", name)
		}
		w.Header().Set("X-Test", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Get("/api/http", router.HandlerFromHTTP(handler)).SetName("http.handler")
	r.Head("/api/http", router.HandlerFromHTTP(handler)).SetName("http.handler")

	server := httptest.NewServer(adapter.WrappedRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/http")
	if err != nil {
		t.Fatalf("Error while making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected status code %d, got %d", http.StatusCreated, resp.StatusCode)
	}
	if resp.Header.Get("X-Test") != "ok" {
		t.Errorf("Expected X-Test header ok, got %s", resp.Header.Get("X-Test"))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	if string(bodyBytes) != `{"status":"ok"}` {
		t.Errorf("Expected body %s, got %s", `{"status":"ok"}`, string(bodyBytes))
	}

	headReq, err := http.NewRequest(http.MethodHead, server.URL+"/api/http", nil)
	if err != nil {
		t.Fatalf("Error creating HEAD request: %v", err)
	}
	headResp, err := http.DefaultClient.Do(headReq)
	if err != nil {
		t.Fatalf("Error while making HEAD request: %v", err)
	}
	defer headResp.Body.Close()

	if headResp.StatusCode != http.StatusCreated {
		t.Errorf("Expected status code %d, got %d", http.StatusCreated, headResp.StatusCode)
	}
	headBody, err := io.ReadAll(headResp.Body)
	if err != nil {
		t.Fatalf("Error reading HEAD response body: %v", err)
	}
	if len(headBody) != 0 {
		t.Errorf("Expected empty body for HEAD, got %q", string(headBody))
	}
}
