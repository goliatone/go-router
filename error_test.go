package router_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

// mockValidationErrors is a helper type for testing validation error mapping
type mockValidationErrors struct{}

func (m mockValidationErrors) ValidationErrors() []router.ValidationError {
	return []router.ValidationError{
		{Field: "name", Message: "Name is required"},
	}
}

func (m mockValidationErrors) Error() string {
	return "error"
}

func TestWithErrorHandlerMiddleware_Fiber(t *testing.T) {
	// Create a new Fiber adapter server
	app := router.NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New(fiber.Config{
			// App configuration here if needed
		})
	})

	// Use the error handler middleware at the top level
	app.Router().Use(router.WithErrorHandlerMiddleware(
		router.WithEnvironment("development"), // so we get stack traces
		router.WithStackTrace(true),
		router.WithLogger(&testLogger{}), // custom logger for testing if needed
	))

	// Define some test handlers that will trigger different error conditions
	app.Router().Get("/no-error", func(c router.Context) error {
		return c.Send([]byte(`OK`))
	})

	app.Router().Get("/router-error", func(c router.Context) error {
		return router.NewNotFoundError("User not found")
	})

	app.Router().Get("/validation-error-custom", func(c router.Context) error {
		return router.NewValidationError("Custom validation error", []router.ValidationError{
			{
				Field:   "id",
				Message: "must be unique",
			},
		})
	})

	app.Router().Get("/validation-error", func(c router.Context) error {
		// return NewValidationError("validation error", map[string]any{
		// 	"error": "validation",
		// })
		return &mockValidationErrors{}
	})

	app.Router().Get("/internal-error", func(c router.Context) error {
		return errors.New("some unexpected error")
	})

	app.Router().Get("/unauthorized", func(c router.Context) error {
		return router.NewUnauthorizedError("unauthorized access")
	})

	tests := []struct {
		name               string
		path               string
		expectedStatusCode int
		expectedErrorType  string
		expectedMessage    string
		checkStack         bool
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
			expectedErrorType:  string(router.ErrorTypeNotFound),
			expectedMessage:    "User not found",
		},
		{
			name:               "ValidationError",
			path:               "/validation-error",
			expectedStatusCode: http.StatusBadRequest,
			expectedErrorType:  string(router.ErrorTypeValidation),
			expectedMessage:    "Validation failed",
		},
		{
			name:               "NewValidationError",
			path:               "/validation-error-custom",
			expectedStatusCode: http.StatusBadRequest,
			expectedErrorType:  string(router.ErrorTypeValidation),
			expectedMessage:    "Custom validation error",
		},
		{
			name:               "InternalError",
			path:               "/internal-error",
			expectedStatusCode: http.StatusInternalServerError,
			expectedErrorType:  string(router.ErrorTypeInternal),
			expectedMessage:    "An unexpected error occurred",
			checkStack:         true,
		},
		{
			name:               "UnauthorizedError",
			path:               "/unauthorized",
			expectedStatusCode: http.StatusUnauthorized,
			expectedErrorType:  string(router.ErrorTypeUnauthorized),
			expectedMessage:    "unauthorized access",
		},
	}

	// Execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
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

			// Parse ErrorResponse
			var er router.ErrorResponse
			body, _ := io.ReadAll(resp.Body)

			if err := json.Unmarshal(body, &er); err != nil {
				t.Fatalf("failed to unmarshal error response: %v", err)
			}

			if er.Error.Type != tt.expectedErrorType {
				t.Errorf("expected error type %s, got %s", tt.expectedErrorType, er.Error.Type)
			}

			// Check error message
			if !strings.Contains(er.Error.Message, tt.expectedMessage) {
				t.Errorf("expected message to contain '%s', got '%s'", tt.expectedMessage, er.Error.Message)
			}

			// Check stack if required
			if tt.checkStack && len(er.Error.Stack) == 0 {
				t.Error("expected stack trace in development mode, got none")
			}

			// For validation errors, check if we have validation details
			if tt.path == "/validation-error" {
				if len(er.Error.Validation) == 0 {
					t.Error("expected validation errors, got none")
				} else {
					found := false
					for _, v := range er.Error.Validation {
						if v.Field == "name" && v.Message == "Name is required" {
							found = true
							break
						}
					}
					if !found {
						t.Error("expected validation error for field 'name'")
					}
				}
			}
		})
	}
}

// testLogger is a simple logger that can be used to capture logs during tests if needed.
// For now, it just implements the Logger interface and does nothing.
type testLogger struct{}

func (l *testLogger) Debug(format string, args ...any) {}
func (l *testLogger) Info(format string, args ...any)  {}
func (l *testLogger) Error(format string, args ...any) {}
func (l *testLogger) Warn(format string, args ...any)  {}
