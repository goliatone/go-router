package router_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-router"
	"github.com/google/uuid"
)

// Helper function to create bool pointers
func boolPtr(b bool) *bool {
	return &b
}

// Example test structs
type BaseModel struct {
	CreatedAt time.Time `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,notnull" json:"updated_at"`
}

// has-one relationship
type Profile struct {
	ID        int64     `bun:",pk" json:"id"`
	AvatarURL string    `bun:"avatar_url" json:"avatar_url"`
	UserID    int64     `bun:"user_id" json:"user_id"`
	CreatedAt time.Time `bun:"created_at,notnull" json:"created_at"`
}

// has-many relationship
type Post struct {
	ID     int64  `bun:",pk" json:"id"`
	UserID int64  `bun:"user_id" json:"user_id"`
	Title  string `bun:"title,notnull" json:"title"`
}

// belongs-to relationship
type Company struct {
	ID   int64  `bun:",pk" json:"id"`
	Name string `bun:"name,notnull" json:"name"`
}

// many-to-many pivot
type OrderToItem struct {
	OrderID int64  `bun:",pk" json:"order_id"`
	ItemID  int64  `bun:",pk" json:"item_id"`
	Order   *Order `bun:"rel:belongs-to,join:order_id=id" json:"order,omitempty"`
	Item    *Item  `bun:"rel:belongs-to,join:item_id=id" json:"item,omitempty"`
}

// m2m relationship
type Order struct {
	ID    int64  `bun:",pk" json:"id"`
	Items []Item `bun:"m2m:order_to_items,join:Order=Item" json:"items"`
}

type Item struct {
	ID int64 `bun:",pk" json:"id"`
}

// Bug test structs for Task 3
type BugTestM2M struct {
	ID      int64  `bun:",pk" json:"id"`
	Simple  []Item `bun:"m2m:simple_pivot" json:"simple_items"`            // No comma case
	Complex []Item `bun:"m2m:complex_pivot,join:a=b" json:"complex_items"` // With comma case
}

type BugTestRequired struct {
	ID           int64  `bun:",pk" json:"id"`
	RequiredOnly string `bun:"field1,notnull" json:"required_only"`           // Should be required
	ConflictCase string `bun:"field2,notnull" json:"conflict_case,omitempty"` // Should NOT be required (omitempty wins)
	OptionalCase string `bun:"field3" json:"optional_case,omitempty"`         // Should NOT be required
}

// main user model that references many relationships
type User struct {
	BaseModel
	ID       uuid.UUID `bun:"id,pk,notnull" json:"id"`
	Name     string    `bun:"name,notnull" json:"name"`
	Email    string    `bun:"email,notnull" json:"email"`
	Age      int       `bun:"age" json:"age"`
	Password string    `bun:"password" json:"-" crud:"-"`
	// has-one
	Profile Profile `bun:"rel:has-one,join:id=user_id" json:"profile,omitempty"`
	// has-many
	Posts []Post `bun:"rel:has-many,join:id=user_id" json:"posts,omitempty"`
	// belongs-to
	CompanyID int64    `bun:"company_id" json:"company_id"`
	Company   *Company `bun:"rel:belongs-to,join:company_id=id" json:"company,omitempty"`
}

func TestExtractSchemaFromType(t *testing.T) {
	tests := []struct {
		name    string
		inType  reflect.Type
		wantMD  router.SchemaMetadata
		checkFn func(t *testing.T, got router.SchemaMetadata)
	}{
		{
			name:   "simple base model",
			inType: reflect.TypeOf(BaseModel{}),
			checkFn: func(t *testing.T, got router.SchemaMetadata) {
				// We expect 2 properties: created_at, updated_at
				if len(got.Properties) != 2 {
					t.Errorf("expected 2 properties, got %d", len(got.Properties))
				}
				// Check that relationships is empty
				if len(got.Relationships) != 0 {
					t.Errorf("did not expect relationships, got %d", len(got.Relationships))
				}
				if len(got.Required) != 2 {
					t.Errorf("expected 2 required fields, got %d", len(got.Required))
				}
			},
		},
		{
			name:   "user model with relationships",
			inType: reflect.TypeOf(User{}),
			checkFn: func(t *testing.T, got router.SchemaMetadata) {
				// Check a few expected properties
				if _, ok := got.Properties["id"]; !ok {
					t.Errorf("expected 'id' property, not found")
				}
				if _, ok := got.Properties["name"]; !ok {
					t.Errorf("expected 'name' property, not found")
				}
				// We also expect 5 or 6 properties (depending on how you skip BaseModel):
				//   id, name, email, age, password (actually skipped via crud:"-"), created_at, updated_at
				// Check relationships
				if len(got.Relationships) == 0 {
					t.Errorf("expected some relationships, got 0")
				}

				// Check has-one
				rel, ok := got.Relationships["profile"]
				if !ok || rel == nil {
					t.Errorf("expected 'profile' relationship, not found")
				} else {
					if rel.RelationType != "has-one" {
						t.Errorf("expected relationType=has-one, got %s", rel.RelationType)
					}
					if rel.RelatedTypeName != "Profile" {
						t.Errorf("expected RelatedTypeName=Profile, got %s", rel.RelatedTypeName)
					}
					if rel.IsSlice {
						t.Errorf("expected IsSlice=false for has-one")
					}
				}

				// Check has-many
				relPosts, ok := got.Relationships["posts"]
				if !ok || relPosts == nil {
					t.Errorf("expected 'posts' relationship, not found")
				} else {
					if relPosts.RelationType != "has-many" {
						t.Errorf("expected relationType=has-many, got %s", relPosts.RelationType)
					}
					if relPosts.RelatedTypeName != "Post" {
						t.Errorf("expected RelatedTypeName=Post, got %s", relPosts.RelatedTypeName)
					}
					if !relPosts.IsSlice {
						t.Errorf("expected IsSlice=true for has-many")
					}
				}

				// Check belongs-to
				relCompany, ok := got.Relationships["company"]
				if !ok || relCompany == nil {
					t.Errorf("expected 'company' relationship, not found")
				} else {
					if relCompany.RelationType != "belongs-to" {
						t.Errorf("expected relationType=belongs-to, got %s", relCompany.RelationType)
					}
					if relCompany.RelatedTypeName != "Company" {
						t.Errorf("expected RelatedTypeName=Company, got %s", relCompany.RelatedTypeName)
					}
					if relCompany.IsSlice {
						t.Errorf("expected IsSlice=false for belongs-to")
					}
				}
			},
		},
		{
			name:   "order with m2m relationship",
			inType: reflect.TypeOf(Order{}),
			checkFn: func(t *testing.T, got router.SchemaMetadata) {
				// Check properties: id plus relation field
				if len(got.Properties) != 2 {
					t.Errorf("expected properties to include id and items, got %d", len(got.Properties))
				}
				if _, ok := got.Properties["items"]; !ok {
					t.Error("expected relation field 'items' to remain in properties")
				}
				// Check relationships - expect 1
				if len(got.Relationships) != 1 {
					t.Errorf("expected 1 relationship, got %d", len(got.Relationships))
				}
				// Validate the m2m relationship
				rel, ok := got.Relationships["items"]
				if !ok {
					t.Errorf("expected 'items' relationship for m2m, not found")
				} else {
					if rel.RelationType != "many-to-many" {
						t.Errorf("expected relationType=m2m, got %s", rel.RelationType)
					}
					if rel.RelatedTypeName != "Item" {
						t.Errorf("expected RelatedTypeName=Item, got %s", rel.RelatedTypeName)
					}
					if !rel.IsSlice {
						t.Errorf("expected IsSlice=true for m2m items")
					}
					if rel.PivotTable != "order_to_items" {
						t.Errorf("expected PivotTable=order_to_items, got %s", rel.PivotTable)
					}
					if rel.PivotJoin != "Order=Item" {
						t.Errorf("expected PivotJoin=Order=Item, got %s", rel.PivotJoin)
					}
				}
			},
		},
		{
			name:   "m2m pivot table name preservation",
			inType: reflect.TypeOf(BugTestM2M{}),
			checkFn: func(t *testing.T, got router.SchemaMetadata) {
				// Verify simple_pivot (no comma) is preserved
				if rel, ok := got.Relationships["simple_items"]; ok && rel != nil {
					if rel.PivotTable != "simple_pivot" {
						t.Errorf("expected PivotTable=simple_pivot, got %s", rel.PivotTable)
					}
				} else {
					t.Error("expected 'simple_items' relationship")
				}

				// Verify complex_pivot (with comma) still works
				if rel, ok := got.Relationships["complex_items"]; ok && rel != nil {
					if rel.PivotTable != "complex_pivot" {
						t.Errorf("expected PivotTable=complex_pivot, got %s", rel.PivotTable)
					}
					if rel.PivotJoin != "a=b" {
						t.Errorf("expected PivotJoin=a=b, got %s", rel.PivotJoin)
					}
				} else {
					t.Error("expected 'complex_items' relationship")
				}
			},
		},
		{
			name:   "required field consistency with omitempty",
			inType: reflect.TypeOf(BugTestRequired{}),
			checkFn: func(t *testing.T, got router.SchemaMetadata) {
				// Check required_only: should be required
				if prop, ok := got.Properties["required_only"]; ok {
					if !prop.Required {
						t.Error("expected required_only.Required=true")
					}
					if !contains(got.Required, "required_only") {
						t.Error("expected 'required_only' in global Required slice")
					}
				}

				// Check conflict_case: omitempty should win (not required)
				if prop, ok := got.Properties["conflict_case"]; ok {
					if prop.Required {
						t.Error("expected conflict_case.Required=false (omitempty should win)")
					}
					if contains(got.Required, "conflict_case") {
						t.Error("did not expect 'conflict_case' in global Required slice")
					}
				}

				// Check optional_case: should not be required
				if prop, ok := got.Properties["optional_case"]; ok {
					if prop.Required {
						t.Error("expected optional_case.Required=false")
					}
					if contains(got.Required, "optional_case") {
						t.Error("did not expect 'optional_case' in global Required slice")
					}
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := router.ExtractSchemaFromType(tc.inType)
			tc.checkFn(t, got)
		})
	}
}

// Helper function for testing Required slice membership
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// TestPropertyInfoJSONSerialization tests that all PropertyInfo fields are properly serialized to JSON
func TestPropertyInfoJSONSerialization(t *testing.T) {
	// Create a PropertyInfo instance with all new metadata fields populated
	propertyInfo := router.PropertyInfo{
		// Existing fields
		Type:         "string",
		Format:       "uuid",
		Description:  "A unique identifier",
		Required:     true,
		Nullable:     false,
		ReadOnly:     false,
		WriteOnly:    false,
		OriginalName: "UserID",
		Example:      "123e4567-e89b-12d3-a456-426614174000",
		Properties:   map[string]router.PropertyInfo{},
		Items:        nil,

		// New metadata fields
		OriginalType:  "uuid.UUID",
		OriginalKind:  reflect.String,
		AllTags:       map[string]string{"json": "user_id", "bun": "user_id,pk", "validate": "required"},
		TransformPath: []string{"struct", "field"},
		GoPackage:     "github.com/google/uuid",
		CustomTagData: map[string]any{"validate": map[string]bool{"required": true}},
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(propertyInfo)
	if err != nil {
		t.Fatalf("Failed to marshal PropertyInfo to JSON: %v", err)
	}

	// Parse back to verify structure
	var result map[string]any
	err = json.Unmarshal(jsonBytes, &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Test existing fields
	expectedFields := map[string]any{
		"type":          "string",
		"format":        "uuid",
		"description":   "A unique identifier",
		"required":      true,
		"nullable":      false,
		"read_only":     false,
		"write_only":    false,
		"original_name": "UserID",
		"example":       "123e4567-e89b-12d3-a456-426614174000",
	}

	for field, expected := range expectedFields {
		if actual, ok := result[field]; !ok {
			t.Errorf("Expected field '%s' not found in JSON", field)
		} else if actual != expected {
			t.Errorf("Field '%s': expected %v, got %v", field, expected, actual)
		}
	}

	// Test new metadata fields
	newFields := []string{"originalType", "originalKind", "allTags", "transformPath", "goPackage", "customTagData"}
	for _, field := range newFields {
		if _, ok := result[field]; !ok {
			t.Errorf("New metadata field '%s' not found in JSON", field)
		}
	}

	// Verify specific new field values
	if result["originalType"] != "uuid.UUID" {
		t.Errorf("originalType: expected 'uuid.UUID', got %v", result["originalType"])
	}

	if result["originalKind"] != float64(reflect.String) {
		t.Errorf("originalKind: expected %v, got %v", float64(reflect.String), result["originalKind"])
	}

	if result["goPackage"] != "github.com/google/uuid" {
		t.Errorf("goPackage: expected 'github.com/google/uuid', got %v", result["goPackage"])
	}

	// Verify AllTags map
	if allTags, ok := result["allTags"].(map[string]any); ok {
		if allTags["json"] != "user_id" {
			t.Errorf("allTags.json: expected 'user_id', got %v", allTags["json"])
		}
		if allTags["bun"] != "user_id,pk" {
			t.Errorf("allTags.bun: expected 'user_id,pk', got %v", allTags["bun"])
		}
		if allTags["validate"] != "required" {
			t.Errorf("allTags.validate: expected 'required', got %v", allTags["validate"])
		}
	} else {
		t.Error("allTags field is not a map")
	}

	// Verify TransformPath array
	if transformPath, ok := result["transformPath"].([]any); ok {
		if len(transformPath) != 2 {
			t.Errorf("transformPath: expected length 2, got %d", len(transformPath))
		} else {
			if transformPath[0] != "struct" {
				t.Errorf("transformPath[0]: expected 'struct', got %v", transformPath[0])
			}
			if transformPath[1] != "field" {
				t.Errorf("transformPath[1]: expected 'field', got %v", transformPath[1])
			}
		}
	} else {
		t.Error("transformPath field is not an array")
	}

	// Verify CustomTagData nested structure
	if customTagData, ok := result["customTagData"].(map[string]any); ok {
		if validate, ok := customTagData["validate"].(map[string]any); ok {
			if validate["required"] != true {
				t.Errorf("customTagData.validate.required: expected true, got %v", validate["required"])
			}
		} else {
			t.Error("customTagData.validate is not a map")
		}
	} else {
		t.Error("customTagData field is not a map")
	}
}

// TestBackwardCompatibility ensures that existing code continues to work unchanged after PropertyInfo enhancements
func TestBackwardCompatibility(t *testing.T) {
	t.Run("Default ExtractSchemaFromType behavior unchanged", func(t *testing.T) {
		// Test with a simple struct
		type SimpleStruct struct {
			ID    int    `json:"id" bun:"id,pk,notnull"`
			Name  string `json:"name" bun:"name,notnull"`
			Email string `json:"email,omitempty" bun:"email"`
		}

		// Call with no options (existing behavior)
		result := router.ExtractSchemaFromType(reflect.TypeOf(SimpleStruct{}))

		// Verify core functionality works exactly as before
		if result.Name != "SimpleStruct" {
			t.Errorf("Expected Name=SimpleStruct, got %s", result.Name)
		}

		if len(result.Properties) != 3 {
			t.Errorf("Expected 3 properties, got %d", len(result.Properties))
		}

		// Verify required fields logic unchanged
		expectedRequired := []string{"id", "name"}
		if len(result.Required) != len(expectedRequired) {
			t.Errorf("Expected %d required fields, got %d", len(expectedRequired), len(result.Required))
		}

		// Verify existing fields still work
		idProp := result.Properties["id"]
		if idProp.Type != "integer" {
			t.Errorf("Expected id type=integer, got %s", idProp.Type)
		}
		if idProp.Format != "int32" {
			t.Errorf("Expected id format=int32, got %s", idProp.Format)
		}
		if !idProp.Required {
			t.Error("Expected id to be required")
		}
		if idProp.OriginalName != "ID" {
			t.Errorf("Expected OriginalName=ID, got %s", idProp.OriginalName)
		}

		nameProp := result.Properties["name"]
		if nameProp.Type != "string" {
			t.Errorf("Expected name type=string, got %s", nameProp.Type)
		}
		if !nameProp.Required {
			t.Error("Expected name to be required")
		}

		emailProp := result.Properties["email"]
		if emailProp.Required {
			t.Error("Expected email to not be required (omitempty)")
		}
	})

	t.Run("Existing function signature unchanged", func(t *testing.T) {
		type TestStruct struct {
			Field string `json:"field"`
		}

		// Test that we can still call with no options
		result1 := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))
		if result1.Name != "TestStruct" {
			t.Error("Function call with no options failed")
		}

		// Test that we can still call with options (variadic remains compatible)
		opts := router.ExtractSchemaFromTypeOptions{}
		result2 := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)
		if result2.Name != "TestStruct" {
			t.Error("Function call with empty options failed")
		}
	})

	t.Run("New metadata fields default to empty/nil", func(t *testing.T) {
		type TestStruct struct {
			Field string `json:"field"`
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))
		prop := result.Properties["field"]

		// New fields should be empty/nil by default (backward compatible)
		if prop.OriginalType != "" {
			t.Errorf("Expected OriginalType to be empty by default, got %s", prop.OriginalType)
		}
		if prop.OriginalKind != 0 {
			t.Errorf("Expected OriginalKind to be zero by default, got %v", prop.OriginalKind)
		}
		if len(prop.AllTags) != 0 {
			t.Errorf("Expected AllTags to be empty by default, got %v", prop.AllTags)
		}
		if len(prop.TransformPath) != 0 {
			t.Errorf("Expected TransformPath to be empty by default, got %v", prop.TransformPath)
		}
		if prop.GoPackage != "" {
			t.Errorf("Expected GoPackage to be empty by default, got %s", prop.GoPackage)
		}
		if len(prop.CustomTagData) != 0 {
			t.Errorf("Expected CustomTagData to be empty by default, got %v", prop.CustomTagData)
		}
	})

	t.Run("JSON serialization backward compatible", func(t *testing.T) {
		// Create PropertyInfo with only old fields set
		prop := router.PropertyInfo{
			Type:         "string",
			Format:       "uuid",
			Description:  "Test field",
			Required:     true,
			Nullable:     false,
			ReadOnly:     false,
			WriteOnly:    false,
			OriginalName: "TestField",
		}

		// Serialize to JSON
		jsonBytes, err := json.Marshal(prop)
		if err != nil {
			t.Fatalf("Failed to marshal PropertyInfo: %v", err)
		}

		// Parse back
		var result map[string]any
		err = json.Unmarshal(jsonBytes, &result)
		if err != nil {
			t.Fatalf("Failed to unmarshal JSON: %v", err)
		}

		// Verify old fields are present and correct
		if result["type"] != "string" {
			t.Errorf("Expected type=string, got %v", result["type"])
		}
		if result["format"] != "uuid" {
			t.Errorf("Expected format=uuid, got %v", result["format"])
		}
		if result["required"] != true {
			t.Errorf("Expected required=true, got %v", result["required"])
		}

		// New fields should be omitted when empty (omitempty tag)
		newFields := []string{"originalType", "originalKind", "allTags", "transformPath", "goPackage", "customTagData"}
		for _, field := range newFields {
			if _, exists := result[field]; exists {
				t.Errorf("Empty field '%s' should be omitted from JSON, but was present", field)
			}
		}
	})

	t.Run("Complex relationships unchanged", func(t *testing.T) {
		// Use the existing User struct from the test file
		result := router.ExtractSchemaFromType(reflect.TypeOf(User{}))

		// Verify relationships are still extracted correctly
		if len(result.Relationships) == 0 {
			t.Error("Expected relationships to be extracted")
		}

		// Check specific relationship types still work
		if rel, ok := result.Relationships["profile"]; ok && rel != nil {
			if rel.RelationType != "has-one" {
				t.Errorf("Expected profile relation type=has-one, got %s", rel.RelationType)
			}
		} else {
			t.Error("Expected profile relationship to exist")
		}

		if rel, ok := result.Relationships["posts"]; ok && rel != nil {
			if rel.RelationType != "has-many" {
				t.Errorf("Expected posts relation type=has-many, got %s", rel.RelationType)
			}
		} else {
			t.Error("Expected posts relationship to exist")
		}

		if rel, ok := result.Relationships["company"]; ok && rel != nil {
			if rel.RelationType != "belongs-to" {
				t.Errorf("Expected company relation type=belongs-to, got %s", rel.RelationType)
			}
		} else {
			t.Error("Expected company relationship to exist")
		}
	})

	t.Run("Special types still handled correctly", func(t *testing.T) {
		type SpecialTypesStruct struct {
			UUID      uuid.UUID      `json:"uuid"`
			Timestamp time.Time      `json:"timestamp"`
			Optional  *string        `json:"optional,omitempty"`
			Numbers   []int          `json:"numbers"`
			Metadata  map[string]any `json:"metadata"`
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(SpecialTypesStruct{}))

		// UUID should have correct type and format
		if uuidProp, ok := result.Properties["uuid"]; ok {
			if uuidProp.Type != "string" || uuidProp.Format != "uuid" {
				t.Errorf("UUID property: expected type=string format=uuid, got type=%s format=%s", uuidProp.Type, uuidProp.Format)
			}
		} else {
			t.Error("Expected uuid property")
		}

		// Time should have correct type and format
		if timeProp, ok := result.Properties["timestamp"]; ok {
			if timeProp.Type != "string" || timeProp.Format != "date-time" {
				t.Errorf("Timestamp property: expected type=string format=date-time, got type=%s format=%s", timeProp.Type, timeProp.Format)
			}
		} else {
			t.Error("Expected timestamp property")
		}

		// Pointer types should be nullable
		if optProp, ok := result.Properties["optional"]; ok {
			if !optProp.Nullable {
				t.Error("Expected optional property to be nullable")
			}
		} else {
			t.Error("Expected optional property")
		}

		// Arrays should have items
		if numProp, ok := result.Properties["numbers"]; ok {
			if numProp.Type != "array" {
				t.Errorf("Expected numbers type=array, got %s", numProp.Type)
			}
			if numProp.Items == nil {
				t.Error("Expected numbers to have items")
			} else if numProp.Items.Type != "integer" {
				t.Errorf("Expected numbers items type=integer, got %s", numProp.Items.Type)
			}
		} else {
			t.Error("Expected numbers property")
		}

		// Maps should be objects
		if metaProp, ok := result.Properties["metadata"]; ok {
			if metaProp.Type != "object" {
				t.Errorf("Expected metadata type=object, got %s", metaProp.Type)
			}
		} else {
			t.Error("Expected metadata property")
		}
	})

	t.Run("Tag processing priority unchanged", func(t *testing.T) {
		type TagPriorityStruct struct {
			Field1 string `json:"json_name" bun:"bun_name"`
			Field2 string `bun:"bun_only"`
			Field3 string `crud:"crud_only"`
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TagPriorityStruct{}))

		// JSON tag should take priority over bun tag
		if _, ok := result.Properties["json_name"]; !ok {
			t.Error("Expected field with json tag to use json name")
		}
		if _, ok := result.Properties["bun_name"]; ok {
			t.Error("Expected json tag to take priority over bun tag")
		}

		// When no json tag, should use next tag in priority (bun)
		if _, ok := result.Properties["bun_only"]; !ok {
			t.Error("Expected field with bun tag to use bun name when no json tag is present")
		}

		// CRUD tag should be used since it's in priority list
		if _, ok := result.Properties["crud_only"]; !ok {
			t.Error("Expected field with crud tag to use crud name when no higher priority tags are present")
		}
	})
}

// TestExtendedOptions tests the new ExtractSchemaFromTypeOptions fields for Phase 2 Task 2.1
func TestExtendedOptions(t *testing.T) {
	type TestStruct struct {
		ID       int    `json:"id" bun:"id,pk" validate:"required" custom:"metadata"`
		Name     string `json:"name" bun:"name,notnull"`
		internal string // unexported field
	}

	t.Run("Default values work correctly", func(t *testing.T) {
		// Test with empty options struct
		opts := router.ExtractSchemaFromTypeOptions{}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should behave exactly like no options
		defaultResult := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))

		// Compare key properties to ensure identical behavior
		if result.Name != defaultResult.Name {
			t.Errorf("Default options behavior changed: Name %s != %s", result.Name, defaultResult.Name)
		}
		if len(result.Properties) != len(defaultResult.Properties) {
			t.Errorf("Default options behavior changed: Properties count %d != %d", len(result.Properties), len(defaultResult.Properties))
		}
		if len(result.Required) != len(defaultResult.Required) {
			t.Errorf("Default options behavior changed: Required count %d != %d", len(result.Required), len(defaultResult.Required))
		}
	})

	t.Run("New boolean options accept values", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,
		}

		// Should not panic or error when options are set
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Basic validation that it still works
		if result.Name != "TestStruct" {
			t.Errorf("Expected Name=TestStruct, got %s", result.Name)
		}
		if len(result.Properties) == 0 {
			t.Error("Expected some properties to be extracted")
		}
	})

	t.Run("Tag processing options accept values", func(t *testing.T) {
		customTagHandler := func(tag string) any {
			if tag == "required" {
				return map[string]bool{"required": true}
			}
			return nil
		}

		opts := router.ExtractSchemaFromTypeOptions{
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": customTagHandler,
				"custom":   customTagHandler,
			},
			TagPriority: []string{"custom", "json", "bun"},
		}

		// Should not panic or error when options are set
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Basic validation that it still works
		if result.Name != "TestStruct" {
			t.Errorf("Expected Name=TestStruct, got %s", result.Name)
		}
		if len(result.Properties) == 0 {
			t.Error("Expected some properties to be extracted")
		}
	})

	t.Run("Field filtering options accept values", func(t *testing.T) {
		customFieldFilter := func(field reflect.StructField) bool {
			// Custom logic: include all fields except those starting with "internal"
			return !strings.HasPrefix(field.Name, "internal")
		}

		fieldNameTransformer := func(fieldName string) string {
			return "prefix_" + strings.ToLower(fieldName)
		}

		propertyTypeMapper := func(t reflect.Type) router.PropertyInfo {
			if t.Kind() == reflect.String {
				return router.PropertyInfo{Type: "custom_string", Format: "special"}
			}
			return router.PropertyInfo{} // Use default handling
		}

		opts := router.ExtractSchemaFromTypeOptions{
			SkipUnexportedFields: boolPtr(false), // Allow unexported fields
			SkipAnonymousFields:  boolPtr(false), // Allow anonymous fields
			CustomFieldFilter:    customFieldFilter,
			FieldNameTransformer: fieldNameTransformer,
			PropertyTypeMapper:   propertyTypeMapper,
		}

		// Should not panic or error when options are set
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Basic validation that it still works
		if result.Name != "TestStruct" {
			t.Errorf("Expected Name=TestStruct, got %s", result.Name)
		}
		if len(result.Properties) == 0 {
			t.Error("Expected some properties to be extracted")
		}
	})

	t.Run("All options can be set together", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			// Existing options
			GetTableName:      func(t reflect.Type) string { return "custom_table" },
			ToSnakeCasePlural: func(s string) string { return s + "_custom_plural" },
			ToSingular:        func(s string) string { return s + "_custom_singular" },

			// New metadata options
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,

			// Tag processing options
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": func(tag string) any { return tag },
			},
			TagPriority: []string{"validate", "json", "bun"},

			// Field filtering and transformation
			SkipUnexportedFields: boolPtr(false),
			SkipAnonymousFields:  boolPtr(false),
			CustomFieldFilter:    func(field reflect.StructField) bool { return true },
			FieldNameTransformer: func(fieldName string) string { return fieldName },
			PropertyTypeMapper:   func(t reflect.Type) router.PropertyInfo { return router.PropertyInfo{} },
		}

		// Should not panic or error when all options are set
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Basic validation that it still works
		if result.Name != "TestStruct" {
			t.Errorf("Expected Name=TestStruct, got %s", result.Name)
		}
		if len(result.Properties) == 0 {
			t.Error("Expected some properties to be extracted")
		}
	})

	t.Run("Default values match TDD specification", func(t *testing.T) {
		// Test that the default values match the TDD plan specification
		opts := router.ExtractSchemaFromTypeOptions{}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Based on TDD plan, these should be the defaults:
		// - IncludeOriginalNames: false
		// - IncludeOriginalTypes: false
		// - IncludeTagMetadata: false
		// - IncludeTypeMetadata: false
		// - SkipUnexportedFields: true
		// - SkipAnonymousFields: true
		// - TagPriority: []string{"json", "bun", "crud"}

		// Since the options are not yet fully implemented, we just verify
		// that the function works with these default values
		if result.Name != "TestStruct" {
			t.Errorf("Expected Name=TestStruct with defaults, got %s", result.Name)
		}

		// Verify that unexported fields are skipped by default (SkipUnexportedFields: true)
		if _, exists := result.Properties["internal"]; exists {
			t.Error("Expected unexported field 'internal' to be skipped by default")
		}

		// Verify that we have the expected exported fields
		expectedFields := []string{"id", "name"}
		for _, field := range expectedFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected field '%s' to be present", field)
			}
		}
	})
}

// TestCustomTagHandlers tests Phase 4 Task 4.1: Custom Tag Handlers functionality
func TestCustomTagHandlers(t *testing.T) {
	type TestStruct struct {
		ID       int    `json:"id" bun:"id,pk" validate:"required,min=1" custom:"special_metadata"`
		Name     string `json:"name" bun:"name,notnull" validate:"required,min=3,max=50" custom:"name_data"`
		Email    string `json:"email" validate:"email" custom:"contact_info"`
		Optional string `json:"optional,omitempty" validate:"optional" custom:"opt_data"`
	}

	// Define custom tag handler functions
	parseValidationRules := func(tag string) any {
		if tag == "" {
			return nil
		}

		rules := make(map[string]any)
		parts := strings.Split(tag, ",")

		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "required" {
				rules["required"] = true
			} else if part == "email" {
				rules["email"] = true
			} else if part == "optional" {
				rules["optional"] = true
			} else if strings.HasPrefix(part, "min=") {
				if minVal := strings.TrimPrefix(part, "min="); minVal != "" {
					rules["min"] = minVal
				}
			} else if strings.HasPrefix(part, "max=") {
				if maxVal := strings.TrimPrefix(part, "max="); maxVal != "" {
					rules["max"] = maxVal
				}
			}
		}

		if len(rules) == 0 {
			return nil
		}
		return rules
	}

	parseCustomMetadata := func(tag string) any {
		if tag == "" {
			return nil
		}

		// Simple custom metadata parser
		return map[string]any{
			"type":      "custom",
			"value":     tag,
			"processed": true,
			"timestamp": "2024-01-01T00:00:00Z",
		}
	}

	t.Run("Custom tag handlers process tags correctly", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeTagMetadata: true, // Enable to also get AllTags for verification
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": parseValidationRules,
				"custom":   parseCustomMetadata,
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Test ID field with validation rules
		if idProp, ok := result.Properties["id"]; ok {
			if idProp.CustomTagData == nil {
				t.Error("Expected CustomTagData to be populated for id field")
			} else {
				// Check validate tag processing
				if validateData, ok := idProp.CustomTagData["validate"]; ok {
					if rules, ok := validateData.(map[string]any); ok {
						if rules["required"] != true {
							t.Error("Expected validate.required=true for id field")
						}
						if rules["min"] != "1" {
							t.Errorf("Expected validate.min='1' for id field, got %v", rules["min"])
						}
					} else {
						t.Error("Expected validate data to be a map")
					}
				} else {
					t.Error("Expected validate tag data in CustomTagData for id field")
				}

				// Check custom tag processing
				if customData, ok := idProp.CustomTagData["custom"]; ok {
					if metadata, ok := customData.(map[string]any); ok {
						if metadata["type"] != "custom" {
							t.Error("Expected custom.type='custom' for id field")
						}
						if metadata["value"] != "special_metadata" {
							t.Errorf("Expected custom.value='special_metadata' for id field, got %v", metadata["value"])
						}
						if metadata["processed"] != true {
							t.Error("Expected custom.processed=true for id field")
						}
					} else {
						t.Error("Expected custom data to be a map")
					}
				} else {
					t.Error("Expected custom tag data in CustomTagData for id field")
				}
			}
		} else {
			t.Error("Expected id property to exist")
		}

		// Test Name field with complex validation rules
		if nameProp, ok := result.Properties["name"]; ok {
			if nameProp.CustomTagData != nil {
				if validateData, ok := nameProp.CustomTagData["validate"]; ok {
					if rules, ok := validateData.(map[string]any); ok {
						if rules["required"] != true {
							t.Error("Expected validate.required=true for name field")
						}
						if rules["min"] != "3" {
							t.Errorf("Expected validate.min='3' for name field, got %v", rules["min"])
						}
						if rules["max"] != "50" {
							t.Errorf("Expected validate.max='50' for name field, got %v", rules["max"])
						}
					}
				}
			}
		}

		// Test Email field with email validation
		if emailProp, ok := result.Properties["email"]; ok {
			if emailProp.CustomTagData != nil {
				if validateData, ok := emailProp.CustomTagData["validate"]; ok {
					if rules, ok := validateData.(map[string]any); ok {
						if rules["email"] != true {
							t.Error("Expected validate.email=true for email field")
						}
					}
				}
			}
		}
	})

	t.Run("Custom tag handlers with empty tags", func(t *testing.T) {
		type EmptyTagStruct struct {
			Field1 string `json:"field1" validate:""`
			Field2 string `json:"field2" custom:""`
			Field3 string `json:"field3"` // No validate or custom tags
		}

		opts := router.ExtractSchemaFromTypeOptions{
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": parseValidationRules,
				"custom":   parseCustomMetadata,
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(EmptyTagStruct{}), opts)

		// Check that empty tags don't create entries
		for fieldName, prop := range result.Properties {
			if prop.CustomTagData != nil {
				for tagName, tagData := range prop.CustomTagData {
					if tagData != nil {
						t.Errorf("Expected empty tag '%s' for field '%s' to return nil, got %v", tagName, fieldName, tagData)
					}
				}
			}
		}
	})

	t.Run("Custom tag handlers with missing handlers", func(t *testing.T) {
		type TestStructWithUnhandledTag struct {
			Field string `json:"field" validate:"required" unhandled:"some_value"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": parseValidationRules,
				// No handler for "unhandled" tag
			},
		}

		// Should not panic when encountering tags without handlers
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStructWithUnhandledTag{}), opts)

		if fieldProp, ok := result.Properties["field"]; ok {
			if fieldProp.CustomTagData != nil {
				// Should have validate data but not unhandled data
				if _, hasValidate := fieldProp.CustomTagData["validate"]; !hasValidate {
					t.Error("Expected validate tag to be processed")
				}
				if _, hasUnhandled := fieldProp.CustomTagData["unhandled"]; hasUnhandled {
					t.Error("Did not expect unhandled tag to be processed")
				}
			}
		}
	})

	t.Run("Custom tag handlers with nil handlers map", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			CustomTagHandlers: nil, // nil map should not cause panic
		}

		// Should not panic
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should work normally but without custom tag processing
		if len(result.Properties) == 0 {
			t.Error("Expected properties to be extracted even with nil CustomTagHandlers")
		}

		// CustomTagData should be empty/nil for all properties
		for _, prop := range result.Properties {
			if len(prop.CustomTagData) != 0 {
				t.Error("Expected CustomTagData to be empty when CustomTagHandlers is nil")
			}
		}
	})

	t.Run("Custom tag handlers JSON serialization", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": parseValidationRules,
				"custom":   parseCustomMetadata,
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Serialize to JSON
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("Failed to marshal result with CustomTagData: %v", err)
		}

		// Parse back to verify structure
		var parsed map[string]any
		err = json.Unmarshal(jsonBytes, &parsed)
		if err != nil {
			t.Fatalf("Failed to unmarshal JSON: %v", err)
		}

		// Verify that customTagData is present in the JSON
		if properties, ok := parsed["properties"].(map[string]any); ok {
			if idProp, ok := properties["id"].(map[string]any); ok {
				if customTagData, ok := idProp["customTagData"].(map[string]any); ok {
					if validateData, ok := customTagData["validate"].(map[string]any); ok {
						if validateData["required"] != true {
							t.Error("CustomTagData not properly serialized to JSON")
						}
					} else {
						t.Error("Expected validate data in JSON customTagData")
					}
				} else {
					t.Error("Expected customTagData in JSON properties")
				}
			}
		}
	})
}

// TestTagPriorityProcessing tests Phase 4 Task 4.2: Tag Priority Processing functionality
func TestTagPriorityProcessing(t *testing.T) {
	type TestStruct struct {
		Field1 string `xml:"xmlName" json:"jsonName" yaml:"yamlName"`
		Field2 string `bun:"bunName" json:"jsonName2" yaml:"yamlName2"`
		Field3 string `json:"jsonName3" crud:"crudName3"`
		Field4 string `yaml:"yamlName4" xml:"xmlName4"`
		Field5 string `bun:"bunName5"`
	}

	t.Run("Default tag priority (json, bun, crud)", func(t *testing.T) {
		// Test with default tag priority
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))

		// Field1 should use json tag (jsonName) since json has priority over xml and yaml
		if _, ok := result.Properties["jsonName"]; !ok {
			t.Error("Expected field1 to use json tag name 'jsonName' with default priority")
		}
		if _, ok := result.Properties["xmlName"]; ok {
			t.Error("Expected json tag to take priority over xml tag for field1")
		}
		if _, ok := result.Properties["yamlName"]; ok {
			t.Error("Expected json tag to take priority over yaml tag for field1")
		}

		// Field2 should use json tag (jsonName2) since json has priority over bun
		if _, ok := result.Properties["jsonName2"]; !ok {
			t.Error("Expected field2 to use json tag name 'jsonName2' with default priority")
		}
		if _, ok := result.Properties["bunName"]; ok {
			t.Error("Expected json tag to take priority over bun tag for field2")
		}

		// Field3 should use json tag (jsonName3) since json has priority over crud
		if _, ok := result.Properties["jsonName3"]; !ok {
			t.Error("Expected field3 to use json tag name 'jsonName3' with default priority")
		}
		if _, ok := result.Properties["crudName3"]; ok {
			t.Error("Expected json tag to take priority over crud tag for field3")
		}

		// Field4 has no json tag, should use field name (Field4) since yaml and xml are not in default priority
		if _, ok := result.Properties["Field4"]; !ok {
			t.Error("Expected field4 to use field name 'Field4' when no priority tags are present")
		}

		// Field5 should use bun tag (bunName5) since it has bun tag and no json tag
		if _, ok := result.Properties["bunName5"]; !ok {
			t.Error("Expected field5 to use bun tag name 'bunName5' when no json tag is present")
		}
	})

	t.Run("Custom tag priority (yaml, xml, json)", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			TagPriority: []string{"yaml", "xml", "json"},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Field1 should use yaml tag (yamlName) since yaml has highest priority
		if _, ok := result.Properties["yamlName"]; !ok {
			t.Error("Expected field1 to use yaml tag name 'yamlName' with custom priority")
		}
		if _, ok := result.Properties["jsonName"]; ok {
			t.Error("Expected yaml tag to take priority over json tag for field1")
		}
		if _, ok := result.Properties["xmlName"]; ok {
			t.Error("Expected yaml tag to take priority over xml tag for field1")
		}

		// Field2 should use yaml tag (yamlName2) since yaml has highest priority
		if _, ok := result.Properties["yamlName2"]; !ok {
			t.Error("Expected field2 to use yaml tag name 'yamlName2' with custom priority")
		}
		if _, ok := result.Properties["jsonName2"]; ok {
			t.Error("Expected yaml tag to take priority over json tag for field2")
		}

		// Field3 should use json tag (jsonName3) since it has no yaml or xml tags
		if _, ok := result.Properties["jsonName3"]; !ok {
			t.Error("Expected field3 to use json tag name 'jsonName3' when higher priority tags are not present")
		}

		// Field4 should use yaml tag (yamlName4) since yaml has highest priority
		if _, ok := result.Properties["yamlName4"]; !ok {
			t.Error("Expected field4 to use yaml tag name 'yamlName4' with custom priority")
		}
		if _, ok := result.Properties["xmlName4"]; ok {
			t.Error("Expected yaml tag to take priority over xml tag for field4")
		}

		// Field5 has no yaml, xml, or json tags, should use field name
		if _, ok := result.Properties["Field5"]; !ok {
			t.Error("Expected field5 to use field name 'Field5' when no priority tags are present")
		}
	})

	t.Run("Custom tag priority (bun, crud, json)", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			TagPriority: []string{"bun", "crud", "json"},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Field1 has no bun or crud tags, should use json tag
		if _, ok := result.Properties["jsonName"]; !ok {
			t.Error("Expected field1 to use json tag name 'jsonName' when higher priority tags are not present")
		}

		// Field2 should use bun tag (bunName) since bun has highest priority
		if _, ok := result.Properties["bunName"]; !ok {
			t.Error("Expected field2 to use bun tag name 'bunName' with custom priority")
		}
		if _, ok := result.Properties["jsonName2"]; ok {
			t.Error("Expected bun tag to take priority over json tag for field2")
		}

		// Field3 should use crud tag (crudName3) since crud has higher priority than json
		if _, ok := result.Properties["crudName3"]; !ok {
			t.Error("Expected field3 to use crud tag name 'crudName3' with custom priority")
		}
		if _, ok := result.Properties["jsonName3"]; ok {
			t.Error("Expected crud tag to take priority over json tag for field3")
		}

		// Field5 should use bun tag (bunName5) since bun has highest priority
		if _, ok := result.Properties["bunName5"]; !ok {
			t.Error("Expected field5 to use bun tag name 'bunName5' with custom priority")
		}
	})

	t.Run("Tag priority affects omitempty detection", func(t *testing.T) {
		type OmitemptyTestStruct struct {
			Field1 string `json:"jsonName,omitempty" yaml:"yamlName"`
			Field2 string `json:"jsonName2" yaml:"yamlName2,omitempty"`
		}

		// Test with default priority (json first)
		result1 := router.ExtractSchemaFromType(reflect.TypeOf(OmitemptyTestStruct{}))

		// Field1 uses json tag which has omitempty, so should not be required
		if field1Prop, ok := result1.Properties["jsonName"]; ok {
			if field1Prop.Required {
				t.Error("Expected field1 to not be required due to json tag omitempty")
			}
		} else {
			t.Error("Expected field1 to use json tag name")
		}

		// Field2 uses json tag which has no omitempty, so might be required based on other factors
		if _, ok := result1.Properties["jsonName2"]; !ok {
			t.Error("Expected field2 to use json tag name")
		}

		// Test with yaml priority first
		opts := router.ExtractSchemaFromTypeOptions{
			TagPriority: []string{"yaml", "json"},
		}
		result2 := router.ExtractSchemaFromType(reflect.TypeOf(OmitemptyTestStruct{}), opts)

		// Field1 uses yaml tag which has no omitempty, so might be required
		if _, ok := result2.Properties["yamlName"]; !ok {
			t.Error("Expected field1 to use yaml tag name with custom priority")
		}

		// Field2 uses yaml tag which has omitempty, so should not be required
		if field2Prop, ok := result2.Properties["yamlName2"]; ok {
			if field2Prop.Required {
				t.Error("Expected field2 to not be required due to yaml tag omitempty")
			}
		} else {
			t.Error("Expected field2 to use yaml tag name with custom priority")
		}
	})

	t.Run("Empty tag priority list uses field names", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			TagPriority: []string{}, // Empty priority list
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// All fields should use their Go field names when no tag priority is set
		expectedFields := []string{"Field1", "Field2", "Field3", "Field4", "Field5"}
		for _, fieldName := range expectedFields {
			if _, ok := result.Properties[fieldName]; !ok {
				t.Errorf("Expected field '%s' to use field name when tag priority is empty", fieldName)
			}
		}
	})

	t.Run("Non-existent tag in priority list", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			TagPriority: []string{"nonexistent", "json", "bun"},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should fall back to next priority (json) since "nonexistent" tag doesn't exist
		// Field1 should use json tag
		if _, ok := result.Properties["jsonName"]; !ok {
			t.Error("Expected field1 to use json tag when higher priority tag doesn't exist")
		}

		// Field2 should use json tag
		if _, ok := result.Properties["jsonName2"]; !ok {
			t.Error("Expected field2 to use json tag when higher priority tag doesn't exist")
		}
	})
}

// TestCustomFieldFiltering tests Phase 4 Task 4.3: Custom Field Filtering functionality
func TestCustomFieldFiltering(t *testing.T) {
	type TestStruct struct {
		ID              int    `json:"id" bun:"id,pk"`
		Name            string `json:"name" bun:"name,notnull"`
		Email           string `json:"email" bun:"email"`
		internalField   string // unexported field
		InternalData    string `json:"internal_data"`    // exported but has "internal" prefix
		DebugInfo       string `json:"debug_info"`       // should be filtered by custom filter
		ProductionField string `json:"production_field"` // should pass custom filter
	}

	t.Run("Default behavior: skip unexported fields", func(t *testing.T) {
		// Default options should skip unexported fields
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))

		// Should not include unexported field
		if _, exists := result.Properties["internalField"]; exists {
			t.Error("Expected unexported field 'internalField' to be skipped by default")
		}

		// Should include exported fields
		expectedFields := []string{"id", "name", "email", "internal_data", "debug_info", "production_field"}
		for _, field := range expectedFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected exported field '%s' to be included by default", field)
			}
		}
	})

	t.Run("SkipUnexportedFields=false: include unexported fields", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			SkipUnexportedFields: boolPtr(false), // Include unexported fields
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should include unexported field now
		if _, exists := result.Properties["internalField"]; !exists {
			t.Error("Expected unexported field 'internalField' to be included when SkipUnexportedFields=false")
		}

		// Should still include all exported fields
		expectedFields := []string{"id", "name", "email", "internal_data", "debug_info", "production_field"}
		for _, field := range expectedFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected exported field '%s' to be included", field)
			}
		}
	})

	t.Run("CustomFieldFilter: exclude fields with internal prefix", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			SkipUnexportedFields: boolPtr(false), // Include unexported fields
			CustomFieldFilter: func(field reflect.StructField) bool {
				// Custom logic: exclude fields that start with "internal" (case-insensitive)
				return !strings.HasPrefix(strings.ToLower(field.Name), "internal")
			},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should exclude fields starting with "internal"
		excludedFields := []string{"internalField", "internal_data"}
		for _, field := range excludedFields {
			if _, exists := result.Properties[field]; exists {
				t.Errorf("Expected field '%s' to be excluded by custom filter", field)
			}
		}

		// Should include other fields
		expectedFields := []string{"id", "name", "email", "debug_info", "production_field"}
		for _, field := range expectedFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected field '%s' to be included", field)
			}
		}
	})

	t.Run("CustomFieldFilter: only production fields", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			CustomFieldFilter: func(field reflect.StructField) bool {
				// Only include fields that contain "production" or are basic required fields
				fieldName := strings.ToLower(field.Name)
				return strings.Contains(fieldName, "production") ||
					fieldName == "id" ||
					fieldName == "name"
			},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should only include ID, Name, and ProductionField
		expectedFields := []string{"id", "name", "production_field"}
		if len(result.Properties) != len(expectedFields) {
			t.Errorf("Expected exactly %d fields, got %d", len(expectedFields), len(result.Properties))
		}

		for _, field := range expectedFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected field '%s' to be included by custom filter", field)
			}
		}

		// Should exclude other fields
		excludedFields := []string{"email", "internal_data", "debug_info"}
		for _, field := range excludedFields {
			if _, exists := result.Properties[field]; exists {
				t.Errorf("Expected field '%s' to be excluded by custom filter", field)
			}
		}
	})

	t.Run("CustomFieldFilter combined with SkipUnexportedFields", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			SkipUnexportedFields: boolPtr(true), // Skip unexported fields (default)
			CustomFieldFilter: func(field reflect.StructField) bool {
				// Additional filtering: exclude debug fields
				return !strings.Contains(strings.ToLower(field.Name), "debug")
			},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should not include unexported field (filtered by SkipUnexportedFields)
		if _, exists := result.Properties["internalField"]; exists {
			t.Error("Expected unexported field 'internalField' to be skipped")
		}

		// Should not include debug field (filtered by CustomFieldFilter)
		if _, exists := result.Properties["debug_info"]; exists {
			t.Error("Expected 'debug_info' field to be excluded by custom filter")
		}

		// Should include other exported fields
		expectedFields := []string{"id", "name", "email", "internal_data", "production_field"}
		for _, field := range expectedFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected field '%s' to be included", field)
			}
		}
	})

	t.Run("CustomFieldFilter with anonymous fields", func(t *testing.T) {
		type EmbeddedStruct struct {
			EmbeddedField string `json:"embedded_field"`
		}

		type TestWithEmbedded struct {
			EmbeddedStruct        // anonymous field
			ID             int    `json:"id"`
			Name           string `json:"name"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			SkipAnonymousFields: boolPtr(false), // Include anonymous fields
			CustomFieldFilter: func(field reflect.StructField) bool {
				// Custom filter: exclude anonymous fields manually
				return !field.Anonymous
			},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestWithEmbedded{}), opts)

		// Should not include the anonymous field despite SkipAnonymousFields=false
		// because CustomFieldFilter excludes it
		if _, exists := result.Properties["EmbeddedStruct"]; exists {
			t.Error("Expected anonymous field 'EmbeddedStruct' to be excluded by custom filter")
		}

		// Should include non-anonymous fields
		expectedFields := []string{"id", "name"}
		for _, field := range expectedFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected field '%s' to be included", field)
			}
		}
	})

	t.Run("CustomFieldFilter returns false for all fields", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			CustomFieldFilter: func(field reflect.StructField) bool {
				// Exclude all fields
				return false
			},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should have no properties
		if len(result.Properties) != 0 {
			t.Errorf("Expected no properties when CustomFieldFilter excludes all fields, got %d", len(result.Properties))
		}

		// Should have no relationships
		if len(result.Relationships) != 0 {
			t.Errorf("Expected no relationships when CustomFieldFilter excludes all fields, got %d", len(result.Relationships))
		}

		// Should have no required fields
		if len(result.Required) != 0 {
			t.Errorf("Expected no required fields when CustomFieldFilter excludes all fields, got %d", len(result.Required))
		}
	})

	t.Run("CustomFieldFilter returns true for all fields", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			SkipUnexportedFields: boolPtr(false), // Include unexported fields
			CustomFieldFilter: func(field reflect.StructField) bool {
				// Include all fields
				return true
			},
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should include all fields (both exported and unexported)
		allFields := []string{"id", "name", "email", "internalField", "internal_data", "debug_info", "production_field"}
		for _, field := range allFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected field '%s' to be included when CustomFieldFilter includes all fields", field)
			}
		}
	})

	t.Run("CustomFieldFilter with nil function", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			SkipUnexportedFields: boolPtr(true), // Explicitly set to true since providing options overrides defaults
			CustomFieldFilter:    nil,           // nil function should not cause panic
		}
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should work normally without custom filtering
		if len(result.Properties) == 0 {
			t.Error("Expected properties to be extracted even with nil CustomFieldFilter")
		}

		// Should behave like default (include exported fields only)
		if _, exists := result.Properties["internalField"]; exists {
			t.Error("Expected unexported field to be skipped with nil CustomFieldFilter")
		}

		expectedFields := []string{"id", "name", "email", "internal_data", "debug_info", "production_field"}
		for _, field := range expectedFields {
			if _, exists := result.Properties[field]; !exists {
				t.Errorf("Expected exported field '%s' to be included with nil CustomFieldFilter", field)
			}
		}
	})
}

// TestCustomPropertyTypeMapping tests Phase 4 Task 4.5: Custom Property Type Mapping functionality
func TestCustomPropertyTypeMapping(t *testing.T) {
	// Define a custom type to test mapping
	type CustomType struct {
		Value string `json:"value"`
	}

	type TestStruct struct {
		ID           int        `json:"id"`
		Name         string     `json:"name"`
		CustomField  CustomType `json:"custom_field"`
		StringField  string     `json:"string_field"`
		IntField     int        `json:"int_field"`
		PointerField *string    `json:"pointer_field"`
	}

	t.Run("Custom property type mapper overrides default behavior", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			PropertyTypeMapper: func(t reflect.Type) router.PropertyInfo {
				if t == reflect.TypeOf(CustomType{}) {
					return router.PropertyInfo{Type: "custom", Format: "special"}
				}
				return router.PropertyInfo{} // Use default handling
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Test that CustomType field uses custom mapping
		if customProp, ok := result.Properties["custom_field"]; ok {
			if customProp.Type != "custom" {
				t.Errorf("Expected custom_field type='custom', got %s", customProp.Type)
			}
			if customProp.Format != "special" {
				t.Errorf("Expected custom_field format='special', got %s", customProp.Format)
			}
		} else {
			t.Error("Expected custom_field property to exist")
		}

		// Test that other fields use default behavior
		if stringProp, ok := result.Properties["string_field"]; ok {
			if stringProp.Type != "string" {
				t.Errorf("Expected string_field to use default type='string', got %s", stringProp.Type)
			}
		} else {
			t.Error("Expected string_field property to exist")
		}

		if intProp, ok := result.Properties["int_field"]; ok {
			if intProp.Type != "integer" {
				t.Errorf("Expected int_field to use default type='integer', got %s", intProp.Type)
			}
		} else {
			t.Error("Expected int_field property to exist")
		}
	})

	t.Run("Custom property type mapper for multiple types", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			PropertyTypeMapper: func(t reflect.Type) router.PropertyInfo {
				switch t {
				case reflect.TypeOf(CustomType{}):
					return router.PropertyInfo{Type: "custom_object", Format: "special_format"}
				case reflect.TypeOf(""):
					return router.PropertyInfo{Type: "custom_string", Format: "text"}
				case reflect.TypeOf(0):
					return router.PropertyInfo{Type: "custom_integer", Format: "number"}
				default:
					return router.PropertyInfo{} // Use default handling
				}
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Test CustomType mapping
		if customProp, ok := result.Properties["custom_field"]; ok {
			if customProp.Type != "custom_object" {
				t.Errorf("Expected custom_field type='custom_object', got %s", customProp.Type)
			}
			if customProp.Format != "special_format" {
				t.Errorf("Expected custom_field format='special_format', got %s", customProp.Format)
			}
		}

		// Test string mapping
		if stringProp, ok := result.Properties["string_field"]; ok {
			if stringProp.Type != "custom_string" {
				t.Errorf("Expected string_field type='custom_string', got %s", stringProp.Type)
			}
			if stringProp.Format != "text" {
				t.Errorf("Expected string_field format='text', got %s", stringProp.Format)
			}
		}

		// Test integer mapping
		if intProp, ok := result.Properties["int_field"]; ok {
			if intProp.Type != "custom_integer" {
				t.Errorf("Expected int_field type='custom_integer', got %s", intProp.Type)
			}
			if intProp.Format != "number" {
				t.Errorf("Expected int_field format='number', got %s", intProp.Format)
			}
		}
	})

	t.Run("Custom property type mapper returns empty PropertyInfo uses default", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			PropertyTypeMapper: func(t reflect.Type) router.PropertyInfo {
				// Always return empty PropertyInfo to use default behavior
				return router.PropertyInfo{}
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// All fields should use default behavior
		if stringProp, ok := result.Properties["string_field"]; ok {
			if stringProp.Type != "string" {
				t.Errorf("Expected string_field to use default type='string', got %s", stringProp.Type)
			}
		}

		if intProp, ok := result.Properties["int_field"]; ok {
			if intProp.Type != "integer" {
				t.Errorf("Expected int_field to use default type='integer', got %s", intProp.Type)
			}
		}

		if customProp, ok := result.Properties["custom_field"]; ok {
			if customProp.Type != "object" {
				t.Errorf("Expected custom_field to use default type='object', got %s", customProp.Type)
			}
		}
	})

	t.Run("Custom property type mapper with pointer types", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			PropertyTypeMapper: func(t reflect.Type) router.PropertyInfo {
				// Handle pointer to string specially
				if t == reflect.TypeOf((*string)(nil)) {
					return router.PropertyInfo{Type: "custom_pointer_string", Format: "nullable_text"}
				}
				return router.PropertyInfo{} // Use default handling
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Test that pointer field uses custom mapping
		if pointerProp, ok := result.Properties["pointer_field"]; ok {
			if pointerProp.Type != "custom_pointer_string" {
				t.Errorf("Expected pointer_field type='custom_pointer_string', got %s", pointerProp.Type)
			}
			if pointerProp.Format != "nullable_text" {
				t.Errorf("Expected pointer_field format='nullable_text', got %s", pointerProp.Format)
			}
		} else {
			t.Error("Expected pointer_field property to exist")
		}

		// Test that other fields use default behavior
		if stringProp, ok := result.Properties["string_field"]; ok {
			if stringProp.Type != "string" {
				t.Errorf("Expected string_field to use default type='string', got %s", stringProp.Type)
			}
		}
	})

	t.Run("Custom property type mapper preserves other PropertyInfo fields", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			PropertyTypeMapper: func(t reflect.Type) router.PropertyInfo {
				if t == reflect.TypeOf(CustomType{}) {
					return router.PropertyInfo{
						Type:        "custom",
						Format:      "special",
						Description: "Custom type mapping",
						Example:     "example_value",
					}
				}
				return router.PropertyInfo{} // Use default handling
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Test that custom mapping preserves all set fields
		if customProp, ok := result.Properties["custom_field"]; ok {
			if customProp.Type != "custom" {
				t.Errorf("Expected custom_field type='custom', got %s", customProp.Type)
			}
			if customProp.Format != "special" {
				t.Errorf("Expected custom_field format='special', got %s", customProp.Format)
			}
			if customProp.Description != "Custom type mapping" {
				t.Errorf("Expected custom_field description='Custom type mapping', got %s", customProp.Description)
			}
			if customProp.Example != "example_value" {
				t.Errorf("Expected custom_field example='example_value', got %v", customProp.Example)
			}
		} else {
			t.Error("Expected custom_field property to exist")
		}
	})

	t.Run("Custom property type mapper with nil function", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			PropertyTypeMapper: nil, // nil function should not cause panic
		}

		// Should not panic
		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Should work normally with default behavior
		if len(result.Properties) == 0 {
			t.Error("Expected properties to be extracted even with nil PropertyTypeMapper")
		}

		// All fields should use default behavior
		if stringProp, ok := result.Properties["string_field"]; ok {
			if stringProp.Type != "string" {
				t.Errorf("Expected string_field to use default type='string', got %s", stringProp.Type)
			}
		}

		if customProp, ok := result.Properties["custom_field"]; ok {
			if customProp.Type != "object" {
				t.Errorf("Expected custom_field to use default type='object', got %s", customProp.Type)
			}
		}
	})

	t.Run("Custom property type mapper with metadata options", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalTypes: true,
			IncludeTypeMetadata:  true,
			PropertyTypeMapper: func(t reflect.Type) router.PropertyInfo {
				if t == reflect.TypeOf(CustomType{}) {
					return router.PropertyInfo{Type: "custom", Format: "special"}
				}
				return router.PropertyInfo{} // Use default handling
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Test that custom mapping works with metadata options
		if customProp, ok := result.Properties["custom_field"]; ok {
			// Custom mapping should override Type and Format
			if customProp.Type != "custom" {
				t.Errorf("Expected custom_field type='custom', got %s", customProp.Type)
			}
			if customProp.Format != "special" {
				t.Errorf("Expected custom_field format='special', got %s", customProp.Format)
			}

			// But metadata should still be added
			if customProp.OriginalType == "" {
				t.Error("Expected OriginalType to be set when IncludeOriginalTypes=true")
			}
		} else {
			t.Error("Expected custom_field property to exist")
		}
	})

	t.Run("Custom property type mapper JSON serialization", func(t *testing.T) {
		opts := router.ExtractSchemaFromTypeOptions{
			PropertyTypeMapper: func(t reflect.Type) router.PropertyInfo {
				if t == reflect.TypeOf(CustomType{}) {
					return router.PropertyInfo{Type: "custom", Format: "special"}
				}
				return router.PropertyInfo{} // Use default handling
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

		// Serialize to JSON
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("Failed to marshal result with custom property mapping: %v", err)
		}

		// Parse back to verify structure
		var parsed map[string]any
		err = json.Unmarshal(jsonBytes, &parsed)
		if err != nil {
			t.Fatalf("Failed to unmarshal JSON: %v", err)
		}

		// Verify that custom type mapping is present in the JSON
		if properties, ok := parsed["properties"].(map[string]any); ok {
			if customProp, ok := properties["custom_field"].(map[string]any); ok {
				if customProp["type"] != "custom" {
					t.Error("Custom type mapping not properly serialized to JSON")
				}
				if customProp["format"] != "special" {
					t.Error("Custom format mapping not properly serialized to JSON")
				}
			} else {
				t.Error("Expected custom_field in JSON properties")
			}
		}
	})
}
