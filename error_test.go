package router_test

import (
	"encoding/json"
	stdErrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-errors"
	"github.com/goliatone/go-router"
)

func TestWithErrorHandlerMiddleware_Fiber(t *testing.T) {
	app := router.NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New(fiber.Config{})
	})

	app.Router().Use(router.WithErrorHandlerMiddleware(
		router.WithEnvironment("development"), // to get stack traces
		router.WithStackTrace(true),
		router.WithLogger(&testLogger{}),
	))

	app.Router().Get("/no-error", func(c router.Context) error {
		return c.Send([]byte(`OK`))
	})

	app.Router().Get("/router-error", func(c router.Context) error {
		return router.NewNotFoundError("User not found")
	})

	app.Router().Get("/validation-error-custom", func(c router.Context) error {
		return router.NewValidationError("Custom validation error", []errors.FieldError{
			{
				Field:   "id",
				Message: "must be unique",
			},
		})
	})

	app.Router().Get("/validation-error", func(c router.Context) error {
		return router.NewValidationError("Validation failed", []errors.FieldError{
			{Field: "name", Message: "Name is required", Value: nil},
		})
	})

	app.Router().Get("/internal-error", func(c router.Context) error {
		return stdErrors.New("some unexpected error")
	})

	app.Router().Get("/unauthorized", func(c router.Context) error {
		return router.NewUnauthorizedError("unauthorized access")
	})

	app.Router().Get("/error-with-metadata", func(c router.Context) error {
		return router.NewNotFoundError("Resource not found",
			map[string]any{
				"resource_id":   "123",
				"resource_type": "user",
			})
	})

	app.Router().Get("/conflict-error", func(c router.Context) error {
		return router.NewConflictError("Resource already exists",
			map[string]any{"existing_id": "456"})
	})

	tests := []struct {
		name               string
		path               string
		expectedStatusCode int
		expectedCategory   errors.Category
		expectedTextCode   string
		expectedMessage    string
		checkValidation    bool
	}{
		{
			name:               "NoError",
			path:               "/no-error",
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "RouterError",
			path:               "/router-error",
			expectedStatusCode: http.StatusNotFound,
			expectedCategory:   errors.CategoryNotFound,
			expectedTextCode:   "NOT_FOUND",
			expectedMessage:    "The requested resource was not found.",
		},
		{
			name:               "ValidationError",
			path:               "/validation-error",
			expectedStatusCode: http.StatusBadRequest,
			expectedCategory:   errors.CategoryValidation,
			expectedTextCode:   "VALIDATION_ERROR",
			expectedMessage:    "The request is invalid.",
			checkValidation:    true,
		},
		{
			name:               "NewValidationError",
			path:               "/validation-error-custom",
			expectedStatusCode: http.StatusBadRequest,
			expectedCategory:   errors.CategoryValidation,
			expectedTextCode:   "VALIDATION_ERROR",
			expectedMessage:    "The request is invalid.",
			checkValidation:    true,
		},
		{
			name:               "InternalError",
			path:               "/internal-error",
			expectedStatusCode: http.StatusInternalServerError,
			expectedCategory:   errors.CategoryInternal,
			expectedTextCode:   "",
			expectedMessage:    "An unexpected error occurred",
		},
		{
			name:               "UnauthorizedError",
			path:               "/unauthorized",
			expectedStatusCode: http.StatusUnauthorized,
			expectedCategory:   errors.CategoryAuth,
			expectedTextCode:   "UNAUTHORIZED",
			expectedMessage:    "Authentication failed.",
		},
		{
			name:               "ErrorWithMetadata",
			path:               "/error-with-metadata",
			expectedStatusCode: http.StatusNotFound,
			expectedCategory:   errors.CategoryNotFound,
			expectedTextCode:   "NOT_FOUND",
			expectedMessage:    "The requested resource was not found.",
		},
		{
			name:               "ConflictError",
			path:               "/conflict-error",
			expectedStatusCode: http.StatusConflict,
			expectedCategory:   errors.CategoryConflict,
			expectedTextCode:   "CONFLICT",
			expectedMessage:    "The request conflicts with the current state.",
		},
	}

	// Execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), "GET", tt.path, nil)
			req.Header.Set("X-Request-ID", "test-request-123")

			resp, err := app.WrappedRouter().Test(req, -1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != tt.expectedStatusCode {
				t.Fatalf("expected status %d, got %d", tt.expectedStatusCode, resp.StatusCode)
			}

			// If this route did not produce an error, just check the body
			if tt.expectedStatusCode == http.StatusOK {
				body, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					t.Fatalf("failed to read response body: %v", readErr)
				}
				if closeErr := resp.Body.Close(); closeErr != nil {
					t.Fatalf("failed to close response body: %v", closeErr)
				}
				if string(body) != "OK" {
					t.Errorf("expected body OK, got %s", string(body))
				}
				return
			}

			// Parse ErrorResponse using our unified error structure
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				t.Fatalf("failed to read error response body: %v", readErr)
			}
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Fatalf("failed to close response body: %v", closeErr)
			}
			var er errors.ErrorResponse
			if err := json.Unmarshal(body, &er); err != nil {
				t.Fatalf("failed to unmarshal error response: %v, body: %s", err, string(body))
			}
			if er.Error == nil {
				t.Fatalf("expected error response payload, body: %s", string(body))
			}

			var envelope struct {
				Error map[string]json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatalf("failed to inspect public error response: %v", err)
			}
			for _, field := range []string{"source", "metadata", "timestamp", "location", "stack_trace"} {
				if _, exists := envelope.Error[field]; exists {
					t.Errorf("public error response unexpectedly exposes %q", field)
				}
			}

			// Check error category
			if er.Error.Category != tt.expectedCategory {
				t.Errorf("expected error category %s, got %s", tt.expectedCategory, er.Error.Category)
			}

			// Check text code (if expected)
			if tt.expectedTextCode != "" && er.Error.TextCode != tt.expectedTextCode {
				t.Errorf("expected error text code %s, got %s", tt.expectedTextCode, er.Error.TextCode)
			}

			// Check error message
			if !strings.Contains(er.Error.Message, tt.expectedMessage) {
				t.Errorf("expected message to contain '%s', got '%s'", tt.expectedMessage, er.Error.Message)
			}

			// Check that code matches status code
			if er.Error.Code != tt.expectedStatusCode {
				t.Errorf("expected error code %d, got %d", tt.expectedStatusCode, er.Error.Code)
			}

			// Check request ID is included
			if er.Error.RequestID != "test-request-123" {
				t.Errorf("expected request ID 'test-request-123', got '%s'", er.Error.RequestID)
			}

			// For validation errors, check if we have validation details
			if tt.checkValidation {
				if len(er.Error.ValidationErrors) == 0 {
					t.Error("expected validation errors, got none")
				} else {
					// Check specific validation error based on the test case
					if tt.path == "/validation-error" {
						found := false
						for _, v := range er.Error.ValidationErrors {
							if v.Field == "name" && v.Message == "invalid value" {
								found = true
								break
							}
						}
						if !found {
							t.Error("expected validation error for field 'name'")
						}
					} else if tt.path == "/validation-error-custom" {
						found := false
						for _, v := range er.Error.ValidationErrors {
							if v.Field == "id" && v.Message == "invalid value" {
								found = true
								break
							}
						}
						if !found {
							t.Error("expected validation error for field 'id'")
						}
					}
				}
			}
		})
	}
}

func TestErrorResponseJSONFormat(t *testing.T) {
	err := errors.NewValidation("validation failed",
		errors.FieldError{Field: "email", Message: "required"},
		errors.FieldError{Field: "age", Message: "must be positive", Value: -5},
	).WithCode(400).
		WithTextCode("VALIDATION_ERROR").
		WithRequestID("req-123").
		WithMetadata(map[string]any{
			"user_id": 456,
			"attempt": 3,
		})

	response := err.ToErrorResponse(false, nil)

	data, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		t.Fatalf("failed to marshal error response: %v", marshalErr)
	}

	var unmarshaled map[string]any
	if unmarshalErr := json.Unmarshal(data, &unmarshaled); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal error response: %v", unmarshalErr)
	}

	// Verify the structure
	errorObj, ok := unmarshaled["error"].(map[string]any)
	if !ok {
		t.Fatal("expected 'error' field to be an object")
	}

	expectedFields := []string{"category", "code", "text_code", "message", "validation_errors", "request_id"}
	for _, field := range expectedFields {
		if _, exists := errorObj[field]; !exists {
			t.Errorf("expected field '%s' to exist in error response", field)
		}
	}
	for _, field := range []string{"source", "metadata", "timestamp", "location", "stack_trace"} {
		if _, exists := errorObj[field]; exists {
			t.Errorf("public error response unexpectedly exposes field %q", field)
		}
	}

	// Verify validation errors structure
	validationErrors, ok := errorObj["validation_errors"].([]any)
	if !ok {
		t.Fatal("expected 'validation_errors' to be an array")
	}

	if len(validationErrors) != 2 {
		t.Errorf("expected 2 validation errors, got %d", len(validationErrors))
	}

	// Check first validation error
	firstError, ok := validationErrors[0].(map[string]any)
	if !ok {
		t.Fatal("expected validation error to be an object")
	}

	field, ok := firstError["field"].(string)
	if !ok {
		t.Fatalf("expected validation error field to be a string, got %T", firstError["field"])
	}
	if field != "email" {
		t.Errorf("expected first validation error field to be 'email', got %q", field)
	}
	if _, exists := firstError["value"]; exists {
		t.Error("public validation error unexpectedly exposes rejected value")
	}
}

type testLogger struct {
	logs []logEntry
}

type logEntry struct {
	level   string
	message string
	fields  map[string]any
}

func (l *testLogger) Info(msg string, args ...any)  {}
func (l *testLogger) Debug(msg string, args ...any) {}
func (l *testLogger) Warn(msg string, args ...any)  {}
func (l *testLogger) Error(msg string, fields ...any) {
	logFields := map[string]any{}
	if len(fields) > 0 {
		if value, ok := fields[0].(map[string]any); ok {
			logFields = value
		}
	}
	l.logs = append(l.logs, logEntry{
		level:   "error",
		message: msg,
		fields:  logFields,
	})
}

func (l *testLogger) HasLogWithField(field string, value any) bool {
	for _, log := range l.logs {
		if log.fields != nil {
			if actualValue, exists := log.fields[field]; exists {
				if reflect.DeepEqual(actualValue, value) {
					return true
				}
			}
		}
	}
	return false
}

func TestErrorLogging(t *testing.T) {
	logger := &testLogger{}

	err := router.NewNotFoundError("user not found", map[string]any{
		"user_id": 123,
	}).WithRequestID("req-456")

	ctx := router.NewMockContext()
	ctx.On("Path").Return("/test")
	ctx.On("Method").Return("GET")

	// Test logging
	router.LogError(logger, err, ctx)

	// Verify log entry was created
	if len(logger.logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logger.logs))
	}

	log := logger.logs[0]

	// Check log fields
	expectedFields := map[string]any{
		"category":   errors.CategoryNotFound.String(),
		"text_code":  "NOT_FOUND",
		"code":       404,
		"path":       "/test",
		"method":     "GET",
		"request_id": "req-456",
	}

	for key, expectedValue := range expectedFields {
		if actualValue, exists := log.fields[key]; !exists {
			t.Errorf("expected log field '%s' to exist", key)
		} else if actualValue != expectedValue {
			t.Errorf("expected log field '%s' = %v, got %v", key, expectedValue, actualValue)
		}
	}

	// Check that metadata is logged
	if !logger.HasLogWithField("metadata", err.Metadata) {
		t.Error("expected metadata to be logged")
	}
}
