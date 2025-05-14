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
		checkStack         bool
		checkValidation    bool
		checkMetadata      bool
		expectedMetadata   map[string]any
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
			expectedMessage:    "User not found",
		},
		{
			name:               "ValidationError",
			path:               "/validation-error",
			expectedStatusCode: http.StatusBadRequest,
			expectedCategory:   errors.CategoryValidation,
			expectedTextCode:   "VALIDATION_ERROR",
			expectedMessage:    "Validation failed",
			checkValidation:    true,
		},
		{
			name:               "NewValidationError",
			path:               "/validation-error-custom",
			expectedStatusCode: http.StatusBadRequest,
			expectedCategory:   errors.CategoryValidation,
			expectedTextCode:   "VALIDATION_ERROR",
			expectedMessage:    "Custom validation error",
			checkValidation:    true,
		},
		{
			name:               "InternalError",
			path:               "/internal-error",
			expectedStatusCode: http.StatusInternalServerError,
			expectedCategory:   errors.CategoryInternal,
			expectedTextCode:   "",
			expectedMessage:    "An unexpected error occurred",
			checkStack:         true,
		},
		{
			name:               "UnauthorizedError",
			path:               "/unauthorized",
			expectedStatusCode: http.StatusUnauthorized,
			expectedCategory:   errors.CategoryAuth,
			expectedTextCode:   "UNAUTHORIZED",
			expectedMessage:    "unauthorized access",
		},
		{
			name:               "ErrorWithMetadata",
			path:               "/error-with-metadata",
			expectedStatusCode: http.StatusNotFound,
			expectedCategory:   errors.CategoryNotFound,
			expectedTextCode:   "NOT_FOUND",
			expectedMessage:    "Resource not found",
			checkMetadata:      true,
			expectedMetadata: map[string]any{
				"resource_id":   "123",
				"resource_type": "user",
			},
		},
		{
			name:               "ConflictError",
			path:               "/conflict-error",
			expectedStatusCode: http.StatusConflict,
			expectedCategory:   errors.CategoryConflict,
			expectedTextCode:   "CONFLICT",
			expectedMessage:    "Resource already exists",
			checkMetadata:      true,
			expectedMetadata: map[string]any{
				"existing_id": "456",
			},
		},
	}

	// Execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			req.Header.Set("X-Request-ID", "test-request-123")

			resp, err := app.WrappedRouter().Test(req, -1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatusCode {
				t.Fatalf("expected status %d, got %d", tt.expectedStatusCode, resp.StatusCode)
			}

			// If this route did not produce an error, just check the body
			if tt.expectedStatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				if string(body) != "OK" {
					t.Errorf("expected body OK, got %s", string(body))
				}
				return
			}

			// Parse ErrorResponse using our unified error structure
			body, _ := io.ReadAll(resp.Body)
			var er errors.ErrorResponse
			if err := json.Unmarshal(body, &er); err != nil {
				t.Fatalf("failed to unmarshal error response: %v, body: %s", err, string(body))
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

			// Check timestamp is present and in RFC3339 format
			if er.Error.Timestamp.IsZero() {
				t.Error("expected timestamp to be present")
			}

			// Check stack if required
			if tt.checkStack && len(er.Error.StackTrace) == 0 {
				t.Error("expected stack trace in development mode, got none")
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
							if v.Field == "name" && v.Message == "Name is required" {
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
							if v.Field == "id" && v.Message == "must be unique" {
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

			// Check metadata if required
			if tt.checkMetadata {
				if er.Error.Metadata == nil {
					t.Error("expected metadata to be present")
				} else {
					for key, expectedValue := range tt.expectedMetadata {
						if actualValue, exists := er.Error.Metadata[key]; !exists {
							t.Errorf("expected metadata key '%s' to exist", key)
						} else if actualValue != expectedValue {
							t.Errorf("expected metadata[%s] = %v, got %v", key, expectedValue, actualValue)
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

	var unmarshaled map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &unmarshaled); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal error response: %v", unmarshalErr)
	}

	// Verify the structure
	errorObj, ok := unmarshaled["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'error' field to be an object")
	}

	expectedFields := []string{"category", "code", "text_code", "message", "validation_errors", "metadata", "request_id", "timestamp"}
	for _, field := range expectedFields {
		if _, exists := errorObj[field]; !exists {
			t.Errorf("expected field '%s' to exist in error response", field)
		}
	}

	// Verify validation errors structure
	validationErrors, ok := errorObj["validation_errors"].([]interface{})
	if !ok {
		t.Fatal("expected 'validation_errors' to be an array")
	}

	if len(validationErrors) != 2 {
		t.Errorf("expected 2 validation errors, got %d", len(validationErrors))
	}

	// Check first validation error
	firstError, ok := validationErrors[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected validation error to be an object")
	}

	if firstError["field"].(string) != "email" {
		t.Errorf("expected first validation error field to be 'email', got '%s'", firstError["field"])
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

	l.logs = append(l.logs, logEntry{
		level:   "error",
		message: msg,
		fields:  fields[0].(map[string]any),
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

	ctx := &mockContext{}

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
