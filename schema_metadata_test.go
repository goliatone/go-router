package router_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/goliatone/go-router"
	"github.com/google/uuid"
)

// TestSchemaOriginalNamesAndTypes tests schema extraction with original names and types metadata
func TestSchemaOriginalNamesAndTypes(t *testing.T) {
	type TestStruct struct {
		UserID   int    `json:"user_id"`
		UserName string `json:"userName"`
		Age      int64  `json:"age"`
	}

	t.Run("IncludeOriginalNames enabled", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Verify OriginalName is populated for user_id property
		if prop, ok := result.Properties["user_id"]; ok {
			if prop.OriginalName != "UserID" {
				t.Errorf("Expected OriginalName=UserID for user_id property, got %s", prop.OriginalName)
			}
		} else {
			t.Error("Expected user_id property to exist")
		}

		// Verify OriginalName is populated for userName property
		if prop, ok := result.Properties["userName"]; ok {
			if prop.OriginalName != "UserName" {
				t.Errorf("Expected OriginalName=UserName for userName property, got %s", prop.OriginalName)
			}
		} else {
			t.Error("Expected userName property to exist")
		}
	})

	t.Run("IncludeOriginalTypes enabled", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalTypes: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Verify OriginalType is populated for int field
		if prop, ok := result.Properties["user_id"]; ok {
			if prop.OriginalType != "int" {
				t.Errorf("Expected OriginalType=int for user_id property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Int {
				t.Errorf("Expected OriginalKind=reflect.Int for user_id property, got %v", prop.OriginalKind)
			}
		} else {
			t.Error("Expected user_id property to exist")
		}

		// Verify OriginalType is populated for string field
		if prop, ok := result.Properties["userName"]; ok {
			if prop.OriginalType != "string" {
				t.Errorf("Expected OriginalType=string for userName property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.String {
				t.Errorf("Expected OriginalKind=reflect.String for userName property, got %v", prop.OriginalKind)
			}
		} else {
			t.Error("Expected userName property to exist")
		}

		// Verify OriginalType is populated for int64 field
		if prop, ok := result.Properties["age"]; ok {
			if prop.OriginalType != "int64" {
				t.Errorf("Expected OriginalType=int64 for age property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Int64 {
				t.Errorf("Expected OriginalKind=reflect.Int64 for age property, got %v", prop.OriginalKind)
			}
		} else {
			t.Error("Expected age property to exist")
		}
	})

	t.Run("Both IncludeOriginalNames and IncludeOriginalTypes enabled", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Verify both OriginalName and OriginalType are populated
		if prop, ok := result.Properties["user_id"]; ok {
			if prop.OriginalName != "UserID" {
				t.Errorf("Expected OriginalName=UserID for user_id property, got %s", prop.OriginalName)
			}
			if prop.OriginalType != "int" {
				t.Errorf("Expected OriginalType=int for user_id property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Int {
				t.Errorf("Expected OriginalKind=reflect.Int for user_id property, got %v", prop.OriginalKind)
			}
		} else {
			t.Error("Expected user_id property to exist")
		}
	})

	t.Run("Options disabled by default", func(t *testing.T) {
		// Use default options (should not include original names/types)
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))

		// OriginalName should still be populated (existing behavior)
		if prop, ok := result.Properties["user_id"]; ok {
			if prop.OriginalName != "UserID" {
				t.Errorf("Expected OriginalName=UserID even when option disabled, got %s", prop.OriginalName)
			}
			// But OriginalType and OriginalKind should be empty/zero
			if prop.OriginalType != "" {
				t.Errorf("Expected OriginalType to be empty when option disabled, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != 0 {
				t.Errorf("Expected OriginalKind to be zero when option disabled, got %v", prop.OriginalKind)
			}
		} else {
			t.Error("Expected user_id property to exist")
		}
	})

	t.Run("Complex types", func(t *testing.T) {
		type ComplexStruct struct {
			Pointer   *string        `json:"pointer"`
			Slice     []int          `json:"slice"`
			Map       map[string]any `json:"map"`
			Timestamp time.Time      `json:"timestamp"`
			UUID      uuid.UUID      `json:"uuid"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalTypes: true,
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(ComplexStruct{}), opts)

		// Test pointer type
		if prop, ok := result.Properties["pointer"]; ok {
			if prop.OriginalType != "*string" {
				t.Errorf("Expected OriginalType=*string for pointer property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Ptr {
				t.Errorf("Expected OriginalKind=reflect.Ptr for pointer property, got %v", prop.OriginalKind)
			}
		}

		// Test slice type
		if prop, ok := result.Properties["slice"]; ok {
			if prop.OriginalType != "[]int" {
				t.Errorf("Expected OriginalType=[]int for slice property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Slice {
				t.Errorf("Expected OriginalKind=reflect.Slice for slice property, got %v", prop.OriginalKind)
			}
		}

		// Test map type
		if prop, ok := result.Properties["map"]; ok {
			if prop.OriginalType != "map[string]interface {}" {
				t.Errorf("Expected OriginalType=map[string]interface {} for map property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Map {
				t.Errorf("Expected OriginalKind=reflect.Map for map property, got %v", prop.OriginalKind)
			}
		}

		// Test special types (time.Time)
		if prop, ok := result.Properties["timestamp"]; ok {
			if prop.OriginalType != "time.Time" {
				t.Errorf("Expected OriginalType=time.Time for timestamp property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Struct {
				t.Errorf("Expected OriginalKind=reflect.Struct for timestamp property, got %v", prop.OriginalKind)
			}
		}

		// Test UUID type
		if prop, ok := result.Properties["uuid"]; ok {
			if prop.OriginalType != "uuid.UUID" {
				t.Errorf("Expected OriginalType=uuid.UUID for uuid property, got %s", prop.OriginalType)
			}
			if prop.OriginalKind != reflect.Array {
				t.Errorf("Expected OriginalKind=reflect.Array for uuid property, got %v", prop.OriginalKind)
			}
		}
	})
}
