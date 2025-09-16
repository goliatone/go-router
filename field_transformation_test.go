package router_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goliatone/go-router"
)

// TestFieldNameTransformation tests field name transformation functionality
func TestFieldNameTransformation(t *testing.T) {
	type TestStruct struct {
		ID       int    `json:"user_id" bun:"user_id,pk"`
		UserName string `json:"userName" bun:"user_name,notnull"`
		Email    string `json:"email" bun:"email"`
		Status   string `json:"status" bun:"status"`
	}

	t.Run("Custom field name transformation with prefix", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			FieldNameTransformer: func(fieldName string) string {
				return "prefix_" + strings.ToLower(fieldName)
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Expected transformed field names
		expectedFields := []string{"prefix_user_id", "prefix_username", "prefix_email", "prefix_status"}

		// Check that original field names don't exist
		originalFields := []string{"user_id", "userName", "email", "status"}
		for _, originalField := range originalFields {
			if _, exists := result.Properties[originalField]; exists {
				t.Errorf("Expected original field name '%s' to not exist after transformation", originalField)
			}
		}

		// Check that transformed field names exist
		for _, transformed := range expectedFields {
			if prop, exists := result.Properties[transformed]; !exists {
				t.Errorf("Expected transformed field name '%s' to exist", transformed)
			} else {
				// Verify the property still has correct metadata
				if prop.OriginalName == "" {
					t.Errorf("Expected OriginalName to be preserved for transformed field '%s'", transformed)
				}
			}
		}

		// Check required fields are also transformed
		for _, requiredField := range result.Required {
			if !strings.HasPrefix(requiredField, "prefix_") {
				t.Errorf("Expected required field '%s' to be transformed with prefix", requiredField)
			}
		}
	})

	t.Run("Field name transformation with uppercase", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			FieldNameTransformer: func(fieldName string) string {
				return strings.ToUpper(fieldName)
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Expected transformed field names (uppercase)
		expectedFields := []string{"USER_ID", "USERNAME", "EMAIL", "STATUS"}

		for _, expected := range expectedFields {
			if _, exists := result.Properties[expected]; !exists {
				t.Errorf("Expected transformed field name '%s' to exist", expected)
			}
		}

		// Check that original field names don't exist
		originalFields := []string{"user_id", "userName", "email", "status"}
		for _, originalField := range originalFields {
			if _, exists := result.Properties[originalField]; exists {
				t.Errorf("Expected original field name '%s' to not exist after transformation", originalField)
			}
		}
	})

	t.Run("Field name transformation with snake_case conversion", func(t *testing.T) {
		type CamelCaseStruct struct {
			UserID    int    `json:"userId"`
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
			IsActive  bool   `json:"isActive"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			FieldNameTransformer: func(fieldName string) string {
				// Convert camelCase to snake_case
				var result strings.Builder
				for i, r := range fieldName {
					if i > 0 && r >= 'A' && r <= 'Z' {
						result.WriteByte('_')
					}
					result.WriteRune(r)
				}
				return strings.ToLower(result.String())
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(CamelCaseStruct{}), opts)

		// Expected snake_case field names
		expectedFields := []string{"user_id", "first_name", "last_name", "is_active"}

		for _, expected := range expectedFields {
			if _, exists := result.Properties[expected]; !exists {
				t.Errorf("Expected transformed field name '%s' to exist", expected)
			}
		}

		// Check that original camelCase field names don't exist
		originalFields := []string{"userId", "firstName", "lastName", "isActive"}
		for _, original := range originalFields {
			if _, exists := result.Properties[original]; exists {
				t.Errorf("Expected original field name '%s' to not exist after transformation", original)
			}
		}
	})

	t.Run("Field name transformation with custom tag priority", func(t *testing.T) {
		type TestStructWithMultipleTags struct {
			Field1 string `xml:"xmlName" json:"jsonName" yaml:"yamlName"`
			Field2 string `bun:"bunName" json:"jsonName2"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			TagPriority: []string{"yaml", "xml", "json"},
			FieldNameTransformer: func(fieldName string) string {
				return "transformed_" + fieldName
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStructWithMultipleTags{}), opts)

		// Field1 should use yaml tag (highest priority), then be transformed
		if _, exists := result.Properties["transformed_yamlName"]; !exists {
			t.Error("Expected field1 to use yaml tag and be transformed to 'transformed_yamlName'")
		}

		// Field2 should use json tag (no yaml tag), then be transformed
		if _, exists := result.Properties["transformed_jsonName2"]; !exists {
			t.Error("Expected field2 to use json tag and be transformed to 'transformed_jsonName2'")
		}

		// Original tag names should not exist
		originalFields := []string{"yamlName", "jsonName2", "xmlName", "jsonName", "bunName"}
		for _, original := range originalFields {
			if _, exists := result.Properties[original]; exists {
				t.Errorf("Expected original field name '%s' to not exist after transformation", original)
			}
		}
	})

	t.Run("Field name transformation with field fallback to Go field name", func(t *testing.T) {
		type NoTagStruct struct {
			UserID   int    // No tags, should use Go field name
			UserName string // No tags, should use Go field name
		}

		opts := router.ExtractSchemaFromTypeOptions{
			FieldNameTransformer: func(fieldName string) string {
				return "api_" + strings.ToLower(fieldName)
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(NoTagStruct{}), opts)

		// Expected transformed Go field names
		expectedFields := []string{"api_userid", "api_username"}

		for _, expected := range expectedFields {
			if _, exists := result.Properties[expected]; !exists {
				t.Errorf("Expected transformed Go field name '%s' to exist", expected)
			}
		}

		// Original Go field names should not exist
		originalFields := []string{"UserID", "UserName"}
		for _, original := range originalFields {
			if _, exists := result.Properties[original]; exists {
				t.Errorf("Expected original Go field name '%s' to not exist after transformation", original)
			}
		}
	})

	t.Run("Field name transformation with relationships", func(t *testing.T) {
		type ProfileStruct struct {
			ID     int64 `bun:",pk" json:"id"`
			UserID int64 `bun:"user_id" json:"user_id"`
		}

		type UserStruct struct {
			ID      int64         `bun:",pk" json:"id"`
			Profile ProfileStruct `bun:"rel:has-one,join:id=user_id" json:"profile"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			FieldNameTransformer: func(fieldName string) string {
				return "rel_" + fieldName
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(UserStruct{}), opts)

		// Check that relationship field name is transformed
		if _, exists := result.Relationships["rel_profile"]; !exists {
			t.Error("Expected relationship field name to be transformed to 'rel_profile'")
		}

		// Check that original relationship field name doesn't exist
		if _, exists := result.Relationships["profile"]; exists {
			t.Error("Expected original relationship field name 'profile' to not exist after transformation")
		}

		// Check that regular properties are also transformed
		if _, exists := result.Properties["rel_id"]; !exists {
			t.Error("Expected property field name to be transformed to 'rel_id'")
		}
	})

	t.Run("Field name transformation is identity function", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			FieldNameTransformer: func(fieldName string) string {
				return fieldName // Identity transformation
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should behave exactly like no transformation
		expectedFields := []string{"user_id", "userName", "email", "status"}

		for _, expected := range expectedFields {
			if _, exists := result.Properties[expected]; !exists {
				t.Errorf("Expected field name '%s' to exist with identity transformation", expected)
			}
		}
	})

	t.Run("Field name transformation with nil function", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			FieldNameTransformer: nil, // nil function should not cause panic
		}

		// Should not panic
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should work normally without transformation
		if len(result.Properties) == 0 {
			t.Error("Expected properties to be extracted even with nil FieldNameTransformer")
		}

		// Should use original field names
		expectedFields := []string{"user_id", "userName", "email", "status"}
		for _, expected := range expectedFields {
			if _, exists := result.Properties[expected]; !exists {
				t.Errorf("Expected original field name '%s' to exist with nil FieldNameTransformer", expected)
			}
		}
	})

	t.Run("Field name transformation preserves metadata", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			FieldNameTransformer: func(fieldName string) string {
				return "meta_" + fieldName
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Check that metadata is preserved for transformed field
		if prop, exists := result.Properties["meta_user_id"]; !exists {
			t.Error("Expected transformed field 'meta_user_id' to exist")
		} else {
			// Verify metadata is preserved
			if prop.OriginalName != "ID" {
				t.Errorf("Expected OriginalName='ID' for transformed field, got '%s'", prop.OriginalName)
			}
			if prop.OriginalType == "" {
				t.Error("Expected OriginalType to be preserved for transformed field")
			}
			if len(prop.AllTags) == 0 {
				t.Error("Expected AllTags to be preserved for transformed field")
			}
		}
	})

	t.Run("Field name transformation with empty string result", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			FieldNameTransformer: func(fieldName string) string {
				return "" // Return empty string
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should handle empty string gracefully - might skip fields or use fallback
		// The exact behavior depends on implementation, but should not panic
		// At minimum, the function should complete without error
		if result.Name != "TestStruct" {
			t.Error("Expected function to complete successfully even with empty string transformer")
		}
	})
}
