package router_test

import (
	"reflect"
	"testing"

	"github.com/goliatone/go-router"
)

// TestTagMetadataCollection tests tag metadata collection functionality
func TestTagMetadataCollection(t *testing.T) {
	type TestStruct struct {
		ID int `json:"id" bun:"id,pk" validate:"required" custom:"value"`
	}

	t.Run("IncludeTagMetadata enabled", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTagMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Verify AllTags is populated for id property
		if prop, ok := result.Properties["id"]; ok {
			if len(prop.AllTags) == 0 {
				t.Error("Expected AllTags to be populated when IncludeTagMetadata is enabled")
			}

			expectedTags := map[string]string{
				"json":     "id",
				"bun":      "id,pk",
				"validate": "required",
				"custom":   "value",
			}

			for tag, expectedValue := range expectedTags {
				if actualValue, exists := prop.AllTags[tag]; !exists {
					t.Errorf("Expected tag '%s' to be present in AllTags", tag)
				} else if actualValue != expectedValue {
					t.Errorf("Expected AllTags[%s]=%s, got %s", tag, expectedValue, actualValue)
				}
			}
		} else {
			t.Error("Expected id property to exist")
		}
	})

	t.Run("IncludeTagMetadata disabled by default", func(t *testing.T) {
		// Use default options (should not include tag metadata)
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))

		// AllTags should be empty/nil when option is disabled
		if prop, ok := result.Properties["id"]; ok {
			if len(prop.AllTags) != 0 {
				t.Errorf("Expected AllTags to be empty when option disabled, got %v", prop.AllTags)
			}
		} else {
			t.Error("Expected id property to exist")
		}
	})

	t.Run("Multiple fields with different tags", func(t *testing.T) {
		type MultiFieldStruct struct {
			Field1 string `json:"field1" bun:"field1,notnull"`
			Field2 int    `json:"field2,omitempty" validate:"min=1"`
			Field3 bool   `custom:"special"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTagMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(MultiFieldStruct{}), opts)

		// Check Field1 tags
		if prop, ok := result.Properties["field1"]; ok {
			expectedTags := map[string]string{
				"json": "field1",
				"bun":  "field1,notnull",
			}
			for tag, expectedValue := range expectedTags {
				if actualValue, exists := prop.AllTags[tag]; !exists {
					t.Errorf("Expected tag '%s' to be present in Field1 AllTags", tag)
				} else if actualValue != expectedValue {
					t.Errorf("Expected Field1 AllTags[%s]=%s, got %s", tag, expectedValue, actualValue)
				}
			}
		}

		// Check Field2 tags
		if prop, ok := result.Properties["field2"]; ok {
			expectedTags := map[string]string{
				"json":     "field2,omitempty",
				"validate": "min=1",
			}
			for tag, expectedValue := range expectedTags {
				if actualValue, exists := prop.AllTags[tag]; !exists {
					t.Errorf("Expected tag '%s' to be present in Field2 AllTags", tag)
				} else if actualValue != expectedValue {
					t.Errorf("Expected Field2 AllTags[%s]=%s, got %s", tag, expectedValue, actualValue)
				}
			}
		}

		// Check Field3 tags
		if prop, ok := result.Properties["Field3"]; ok { // No json tag, uses field name
			expectedTags := map[string]string{
				"custom": "special",
			}
			for tag, expectedValue := range expectedTags {
				if actualValue, exists := prop.AllTags[tag]; !exists {
					t.Errorf("Expected tag '%s' to be present in Field3 AllTags", tag)
				} else if actualValue != expectedValue {
					t.Errorf("Expected Field3 AllTags[%s]=%s, got %s", tag, expectedValue, actualValue)
				}
			}
		}
	})

	t.Run("Fields with no tags", func(t *testing.T) {
		type NoTagsStruct struct {
			PlainField string
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTagMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(NoTagsStruct{}), opts)

		// Even when IncludeTagMetadata is enabled, fields with no tags should have empty AllTags
		if prop, ok := result.Properties["PlainField"]; ok {
			if len(prop.AllTags) != 0 {
				t.Errorf("Expected AllTags to be empty for field with no tags, got %v", prop.AllTags)
			}
		} else {
			t.Error("Expected PlainField property to exist")
		}
	})

	t.Run("Empty tag values", func(t *testing.T) {
		type EmptyTagStruct struct {
			Field string `json:"" validate:""`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTagMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(EmptyTagStruct{}), opts)

		// Empty tag values should still be included in AllTags
		if prop, ok := result.Properties["Field"]; ok { // Empty json tag means field name is used
			expectedTags := map[string]string{
				"json":     "",
				"validate": "",
			}
			for tag, expectedValue := range expectedTags {
				if actualValue, exists := prop.AllTags[tag]; !exists {
					t.Errorf("Expected tag '%s' to be present in AllTags even with empty value", tag)
				} else if actualValue != expectedValue {
					t.Errorf("Expected AllTags[%s]=%s, got %s", tag, expectedValue, actualValue)
				}
			}
		}
	})

	t.Run("Integration with existing options", func(t *testing.T) {
		type IntegrationStruct struct {
			UserID   int    `json:"user_id" bun:"user_id,pk" validate:"required"`
			UserName string `json:"userName" validate:"min=1"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(IntegrationStruct{}), opts)

		// Verify all three types of metadata are populated together
		if prop, ok := result.Properties["user_id"]; ok {
			// Original metadata
			if prop.OriginalName != "UserID" {
				t.Errorf("Expected OriginalName=UserID, got %s", prop.OriginalName)
			}
			if prop.OriginalType != "int" {
				t.Errorf("Expected OriginalType=int, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Int {
				t.Errorf("Expected OriginalKind=reflect.Int, got %v", prop.OriginalKind)
			}

			// Tag metadata
			expectedTags := map[string]string{
				"json":     "user_id",
				"bun":      "user_id,pk",
				"validate": "required",
			}
			for tag, expectedValue := range expectedTags {
				if actualValue, exists := prop.AllTags[tag]; !exists {
					t.Errorf("Expected tag '%s' to be present in AllTags", tag)
				} else if actualValue != expectedValue {
					t.Errorf("Expected AllTags[%s]=%s, got %s", tag, expectedValue, actualValue)
				}
			}
		} else {
			t.Error("Expected user_id property to exist")
		}
	})
}
