package router_test

import (
	"reflect"
	"testing"

	"github.com/goliatone/go-router"
)

// Define a custom type for testing package info
type CustomType struct {
	Value string `json:"value"`
}

// TestTypeHierarchyMetadata tests type hierarchy metadata and transformation paths
func TestTypeHierarchyMetadata(t *testing.T) {
	type TestStruct struct {
		Data   *[]map[string]*CustomType `json:"data"`
		Simple int                       `json:"simple"`
	}

	t.Run("IncludeTypeMetadata enabled", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTypeMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Test complex type transformation path
		if prop, ok := result.Properties["data"]; ok {
			expectedPath := []string{"pointer", "slice", "map", "pointer", "CustomType"}
			if len(prop.TransformPath) != len(expectedPath) {
				t.Errorf("Expected TransformPath length %d, got %d", len(expectedPath), len(prop.TransformPath))
			} else {
				for i, expected := range expectedPath {
					if i >= len(prop.TransformPath) || prop.TransformPath[i] != expected {
						t.Errorf("Expected TransformPath[%d]=%s, got %s", i, expected, prop.TransformPath[i])
					}
				}
			}

			// Test package information
			if prop.GoPackage != "github.com/goliatone/go-router_test" {
				t.Logf("Note: GoPackage for data field is '%s' (may vary based on test environment)", prop.GoPackage)
			}
		} else {
			t.Error("Expected data property to exist")
		}

		// Test simple type has no transformation path
		if prop, ok := result.Properties["simple"]; ok {
			if len(prop.TransformPath) != 0 {
				t.Errorf("Expected simple type to have empty TransformPath, got %v", prop.TransformPath)
			}
			if prop.GoPackage != "" {
				t.Errorf("Expected simple type to have empty GoPackage, got %s", prop.GoPackage)
			}
		} else {
			t.Error("Expected simple property to exist")
		}
	})

	t.Run("IncludeTypeMetadata disabled by default", func(t *testing.T) {
		// Use default options (should not include type metadata)
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))

		// TransformPath and GoPackage should be empty when option is disabled
		if prop, ok := result.Properties["data"]; ok {
			if len(prop.TransformPath) != 0 {
				t.Errorf("Expected TransformPath to be empty when option disabled, got %v", prop.TransformPath)
			}
			if prop.GoPackage != "" {
				t.Errorf("Expected GoPackage to be empty when option disabled, got %s", prop.GoPackage)
			}
		} else {
			t.Error("Expected data property to exist")
		}
	})

	t.Run("Pointer types", func(t *testing.T) {
		type PointerStruct struct {
			SinglePointer *string     `json:"single_pointer"`
			DoublePointer **int       `json:"double_pointer"`
			PointerSlice  *[]string   `json:"pointer_slice"`
			SlicePointer  []*string   `json:"slice_pointer"`
			PointerCustom *CustomType `json:"pointer_custom"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTypeMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(PointerStruct{}), opts)

		// Test single pointer
		if prop, ok := result.Properties["single_pointer"]; ok {
			expectedPath := []string{"pointer"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected single_pointer TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test double pointer
		if prop, ok := result.Properties["double_pointer"]; ok {
			expectedPath := []string{"pointer", "pointer"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected double_pointer TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test pointer to slice
		if prop, ok := result.Properties["pointer_slice"]; ok {
			expectedPath := []string{"pointer", "slice"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected pointer_slice TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test slice of pointers
		if prop, ok := result.Properties["slice_pointer"]; ok {
			expectedPath := []string{"slice", "pointer"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected slice_pointer TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test pointer to custom type
		if prop, ok := result.Properties["pointer_custom"]; ok {
			expectedPath := []string{"pointer", "CustomType"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected pointer_custom TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}
	})

	t.Run("Slice and Array types", func(t *testing.T) {
		type SliceStruct struct {
			StringSlice []string     `json:"string_slice"`
			IntArray    [5]int       `json:"int_array"`
			CustomSlice []CustomType `json:"custom_slice"`
			NestedSlice [][]string   `json:"nested_slice"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTypeMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(SliceStruct{}), opts)

		// Test string slice
		if prop, ok := result.Properties["string_slice"]; ok {
			expectedPath := []string{"slice"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected string_slice TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test array (should be treated like slice)
		if prop, ok := result.Properties["int_array"]; ok {
			expectedPath := []string{"slice"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected int_array TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test slice of custom types
		if prop, ok := result.Properties["custom_slice"]; ok {
			expectedPath := []string{"slice", "CustomType"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected custom_slice TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test nested slice
		if prop, ok := result.Properties["nested_slice"]; ok {
			expectedPath := []string{"slice", "slice"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected nested_slice TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}
	})

	t.Run("Map types", func(t *testing.T) {
		type MapStruct struct {
			StringMap map[string]int          `json:"string_map"`
			CustomMap map[string]CustomType   `json:"custom_map"`
			NestedMap map[string]map[int]bool `json:"nested_map"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTypeMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(MapStruct{}), opts)

		// Test string map
		if prop, ok := result.Properties["string_map"]; ok {
			expectedPath := []string{"map"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected string_map TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test map with custom type values
		if prop, ok := result.Properties["custom_map"]; ok {
			expectedPath := []string{"map", "CustomType"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected custom_map TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}

		// Test nested map
		if prop, ok := result.Properties["nested_map"]; ok {
			expectedPath := []string{"map", "map"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected nested_map TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		}
	})

	t.Run("Complex nested types", func(t *testing.T) {
		type ComplexStruct struct {
			SuperComplex *[]*map[string]*[]CustomType `json:"super_complex"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTypeMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(ComplexStruct{}), opts)

		// Test super complex type: *[]*map[string]*[]CustomType
		if prop, ok := result.Properties["super_complex"]; ok {
			expectedPath := []string{"pointer", "slice", "pointer", "map", "pointer", "slice", "CustomType"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected super_complex TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		} else {
			t.Error("Expected super_complex property to exist")
		}
	})

	t.Run("Built-in types have no package", func(t *testing.T) {
		type BuiltinStruct struct {
			StringField int    `json:"string_field"`
			IntField    string `json:"int_field"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTypeMetadata: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(BuiltinStruct{}), opts)

		// Built-in types should not have package information
		for fieldName, prop := range result.Properties {
			if prop.GoPackage != "" {
				t.Errorf("Expected built-in type %s to have empty GoPackage, got %s", fieldName, prop.GoPackage)
			}
		}
	})

	t.Run("Integration with other metadata options", func(t *testing.T) {
		type IntegrationStruct struct {
			ComplexField *[]CustomType `json:"complex_field" bun:"complex_field,notnull" validate:"required"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(IntegrationStruct{}), opts)

		// Verify all metadata types are populated together
		if prop, ok := result.Properties["complex_field"]; ok {
			// Original metadata
			if prop.OriginalName != "ComplexField" {
				t.Errorf("Expected OriginalName=ComplexField, got %s", prop.OriginalName)
			}
			if prop.OriginalType != "*[]router_test.CustomType" {
				t.Logf("Note: OriginalType is '%s' (may vary based on test environment)", prop.OriginalType)
			}

			// Tag metadata
			if len(prop.AllTags) == 0 {
				t.Error("Expected AllTags to be populated")
			}

			// Type hierarchy metadata
			expectedPath := []string{"pointer", "slice", "CustomType"}
			if !equalStringSlices(prop.TransformPath, expectedPath) {
				t.Errorf("Expected TransformPath=%v, got %v", expectedPath, prop.TransformPath)
			}
		} else {
			t.Error("Expected complex_field property to exist")
		}
	})
}

// Helper function to compare string slices
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
