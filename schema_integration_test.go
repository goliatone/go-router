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

// Schema Integration Tests
// This file contains comprehensive integration tests for the enhanced metadata options

// Test complex, deeply nested structures
type DeeplyNestedStruct struct {
	ID          int64                                  `json:"id" bun:"id,pk" validate:"required"`
	MetaData    map[string]*NestedObject               `json:"metadata" bun:"metadata"`
	Collections []*CollectionWithRelations             `json:"collections" bun:"rel:has-many"`
	Pointer     ***TripleNestedPointer                 `json:"pointer" bun:"pointer"`
	Matrix      [][]map[string][]IntegrationCustomType `json:"matrix" bun:"matrix"`
}

type NestedObject struct {
	Name   string                 `json:"name" validate:"required,min=1"`
	Values map[string]interface{} `json:"values"`
	Links  []*LinkObject          `json:"links" bun:"rel:has-many"`
}

type LinkObject struct {
	URL    string    `json:"url" validate:"url"`
	Type   string    `json:"type" validate:"oneof=internal external"`
	Active bool      `json:"active"`
	Tags   []string  `json:"tags"`
	Date   time.Time `json:"date"`
}

type CollectionWithRelations struct {
	ID       int64        `json:"id" bun:"id,pk"`
	Name     string       `json:"name" bun:"name,notnull"`
	Items    []Item       `json:"items" bun:"m2m:collection_items"`
	Parent   *Collection  `json:"parent" bun:"rel:belongs-to"`
	Children []Collection `json:"children" bun:"rel:has-many"`
}

type Collection struct {
	ID   int64  `json:"id" bun:"id,pk"`
	Name string `json:"name" bun:"name,notnull"`
}

type TripleNestedPointer struct {
	Value  *DoubleNestedPointer `json:"value"`
	Extras map[string]*string   `json:"extras"`
}

type DoubleNestedPointer struct {
	Data *SingleNestedPointer `json:"data"`
	Meta *MetaInfo            `json:"meta"`
}

type SingleNestedPointer struct {
	Content string `json:"content"`
	Score   *int   `json:"score,omitempty"`
}

type MetaInfo struct {
	Version   string          `json:"version"`
	Timestamp time.Time       `json:"timestamp"`
	Flags     map[string]bool `json:"flags"`
	Numbers   []int           `json:"numbers"`
	UUIDs     []uuid.UUID     `json:"uuids"`
	Metadata  map[string]any  `json:"metadata"`
}

type IntegrationCustomType struct {
	Type      string     `json:"type" validate:"required"`
	Value     any        `json:"value"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	Tags      []string   `json:"tags"`
}

// Real-world like models from actual projects
type UserAccountModel struct {
	IntegrationBaseModel
	ID                uuid.UUID              `json:"id" bun:"id,pk,type:uuid,default:gen_random_uuid()" validate:"required"`
	Email             string                 `json:"email" bun:"email,unique,notnull" validate:"required,email"`
	Username          string                 `json:"username" bun:"username,unique,notnull" validate:"required,min=3,max=50"`
	PasswordHash      string                 `json:"-" bun:"password_hash,notnull" crud:"-"`
	FirstName         string                 `json:"first_name" bun:"first_name" validate:"required,min=1,max=100"`
	LastName          string                 `json:"last_name" bun:"last_name" validate:"required,min=1,max=100"`
	PhoneNumber       *string                `json:"phone_number,omitempty" bun:"phone_number" validate:"omitempty,phone"`
	DateOfBirth       *time.Time             `json:"date_of_birth,omitempty" bun:"date_of_birth"`
	IsActive          bool                   `json:"is_active" bun:"is_active,default:true"`
	IsVerified        bool                   `json:"is_verified" bun:"is_verified,default:false"`
	LastLoginAt       *time.Time             `json:"last_login_at,omitempty" bun:"last_login_at"`
	PreferencesJSON   map[string]interface{} `json:"preferences" bun:"preferences,type:jsonb"`
	Profile           *UserProfile           `json:"profile,omitempty" bun:"rel:has-one,join:id=user_id"`
	Orders            []Order                `json:"orders,omitempty" bun:"rel:has-many,join:id=user_id"`
	Addresses         []Address              `json:"addresses,omitempty" bun:"rel:has-many,join:id=user_id"`
	PaymentMethods    []PaymentMethod        `json:"payment_methods,omitempty" bun:"rel:has-many,join:id=user_id"`
	Roles             []Role                 `json:"roles,omitempty" bun:"m2m:user_roles,join:User=Role"`
	Teams             []Team                 `json:"teams,omitempty" bun:"m2m:user_teams,join:User=Team"`
	Notifications     []Notification         `json:"notifications,omitempty" bun:"rel:has-many,join:id=user_id"`
	ActivityLogs      []ActivityLog          `json:"activity_logs,omitempty" bun:"rel:has-many,join:id=user_id"`
	internalField     string                 // unexported field for testing
	InternalDebugData string                 `json:"internal_debug_data" internal:"true"`
}

type IntegrationBaseModel struct {
	CreatedAt time.Time  `json:"created_at" bun:"created_at,notnull,default:now()"`
	UpdatedAt time.Time  `json:"updated_at" bun:"updated_at,notnull,default:now()"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" bun:"deleted_at,soft_delete"`
}

type UserProfile struct {
	ID         int64                  `json:"id" bun:"id,pk,autoincrement"`
	UserID     uuid.UUID              `json:"user_id" bun:"user_id,type:uuid,notnull"`
	AvatarURL  *string                `json:"avatar_url,omitempty" bun:"avatar_url"`
	Bio        *string                `json:"bio,omitempty" bun:"bio"`
	Website    *string                `json:"website,omitempty" bun:"website" validate:"omitempty,url"`
	Location   *string                `json:"location,omitempty" bun:"location"`
	SocialMeta map[string]interface{} `json:"social_meta" bun:"social_meta,type:jsonb"`
}

type Address struct {
	ID           int64     `json:"id" bun:"id,pk,autoincrement"`
	UserID       uuid.UUID `json:"user_id" bun:"user_id,type:uuid,notnull"`
	Type         string    `json:"type" bun:"type,notnull" validate:"oneof=billing shipping"`
	Street       string    `json:"street" bun:"street,notnull" validate:"required"`
	City         string    `json:"city" bun:"city,notnull" validate:"required"`
	State        string    `json:"state" bun:"state,notnull" validate:"required"`
	PostalCode   string    `json:"postal_code" bun:"postal_code,notnull" validate:"required"`
	Country      string    `json:"country" bun:"country,notnull" validate:"required,iso3166_1_alpha2"`
	IsDefault    bool      `json:"is_default" bun:"is_default,default:false"`
	Instructions *string   `json:"instructions,omitempty" bun:"instructions"`
}

type PaymentMethod struct {
	ID             int64                  `json:"id" bun:"id,pk,autoincrement"`
	UserID         uuid.UUID              `json:"user_id" bun:"user_id,type:uuid,notnull"`
	Type           string                 `json:"type" bun:"type,notnull" validate:"oneof=card bank paypal crypto"`
	Provider       string                 `json:"provider" bun:"provider,notnull"`
	LastFour       *string                `json:"last_four,omitempty" bun:"last_four"`
	ExpiryMonth    *int                   `json:"expiry_month,omitempty" bun:"expiry_month" validate:"omitempty,min=1,max=12"`
	ExpiryYear     *int                   `json:"expiry_year,omitempty" bun:"expiry_year" validate:"omitempty,min=2024"`
	IsDefault      bool                   `json:"is_default" bun:"is_default,default:false"`
	IsVerified     bool                   `json:"is_verified" bun:"is_verified,default:false"`
	MetadataFields map[string]interface{} `json:"metadata" bun:"metadata,type:jsonb"`
}

type Role struct {
	ID          int64              `json:"id" bun:"id,pk,autoincrement"`
	Name        string             `json:"name" bun:"name,unique,notnull" validate:"required"`
	Description *string            `json:"description,omitempty" bun:"description"`
	Permissions []string           `json:"permissions" bun:"permissions,array"`
	IsActive    bool               `json:"is_active" bun:"is_active,default:true"`
	Level       int                `json:"level" bun:"level,default:1" validate:"min=1,max=10"`
	Parent      *Role              `json:"parent,omitempty" bun:"rel:belongs-to,join:parent_id=id"`
	Children    []Role             `json:"children,omitempty" bun:"rel:has-many,join:id=parent_id"`
	Users       []UserAccountModel `json:"users,omitempty" bun:"m2m:user_roles,join:Role=User"`
}

type Team struct {
	ID          int64                  `json:"id" bun:"id,pk,autoincrement"`
	Name        string                 `json:"name" bun:"name,notnull" validate:"required"`
	Description *string                `json:"description,omitempty" bun:"description"`
	IsActive    bool                   `json:"is_active" bun:"is_active,default:true"`
	Settings    map[string]interface{} `json:"settings" bun:"settings,type:jsonb"`
	Members     []UserAccountModel     `json:"members,omitempty" bun:"m2m:user_teams,join:Team=User"`
	Lead        *UserAccountModel      `json:"lead,omitempty" bun:"rel:belongs-to,join:lead_id=id"`
}

type Notification struct {
	ID        int64                  `json:"id" bun:"id,pk,autoincrement"`
	UserID    uuid.UUID              `json:"user_id" bun:"user_id,type:uuid,notnull"`
	Title     string                 `json:"title" bun:"title,notnull" validate:"required"`
	Message   string                 `json:"message" bun:"message,notnull" validate:"required"`
	Type      string                 `json:"type" bun:"type,notnull" validate:"oneof=info warning error success"`
	IsRead    bool                   `json:"is_read" bun:"is_read,default:false"`
	ReadAt    *time.Time             `json:"read_at,omitempty" bun:"read_at"`
	Data      map[string]interface{} `json:"data,omitempty" bun:"data,type:jsonb"`
	ExpiresAt *time.Time             `json:"expires_at,omitempty" bun:"expires_at"`
}

type ActivityLog struct {
	ID         int64                  `json:"id" bun:"id,pk,autoincrement"`
	UserID     uuid.UUID              `json:"user_id" bun:"user_id,type:uuid,notnull"`
	Action     string                 `json:"action" bun:"action,notnull" validate:"required"`
	Resource   string                 `json:"resource" bun:"resource,notnull" validate:"required"`
	ResourceID *string                `json:"resource_id,omitempty" bun:"resource_id"`
	IPAddress  string                 `json:"ip_address" bun:"ip_address" validate:"ip"`
	UserAgent  string                 `json:"user_agent" bun:"user_agent"`
	Metadata   map[string]interface{} `json:"metadata,omitempty" bun:"metadata,type:jsonb"`
	Success    bool                   `json:"success" bun:"success"`
	ErrorMsg   *string                `json:"error_message,omitempty" bun:"error_message"`
}

// TestDeeplyNestedStructures tests schema extraction with complex nested structures
func TestDeeplyNestedStructures(t *testing.T) {
	t.Run("Complex nested structure with all metadata options", func(t *testing.T) {
		// Custom validation tag handler
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
				} else if strings.HasPrefix(part, "min=") {
					rules["min"] = strings.TrimPrefix(part, "min=")
				} else if strings.HasPrefix(part, "oneof=") {
					rules["oneof"] = strings.Split(strings.TrimPrefix(part, "oneof="), " ")
				}
			}
			return rules
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,
			SkipAnonymousFields:  boolPtr(false), // Include embedded structs
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": parseValidationRules,
			},
			TagPriority: []string{"json", "bun", "validate"},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(DeeplyNestedStruct{}), opts)

		// Verify basic structure
		if result.Name != "DeeplyNestedStruct" {
			t.Errorf("Expected Name=DeeplyNestedStruct, got %s", result.Name)
		}

		// Verify deeply nested properties are handled
		expectedProperties := []string{"id", "metadata", "pointer", "matrix"}
		for _, prop := range expectedProperties {
			if _, exists := result.Properties[prop]; !exists {
				t.Errorf("Expected property '%s' to exist", prop)
			}
		}

		// "collections" should be in relationships, not properties
		if _, exists := result.Relationships["collections"]; !exists {
			t.Error("Expected 'collections' relationship to exist")
		}

		// Test metadata field (map with pointer values)
		if metaProp, ok := result.Properties["metadata"]; ok {
			if metaProp.Type != "object" {
				t.Errorf("Expected metadata type=object, got %s", metaProp.Type)
			}
			if metaProp.OriginalType == "" {
				t.Error("Expected OriginalType to be populated for metadata field")
			}
			if len(metaProp.AllTags) == 0 {
				t.Error("Expected AllTags to be populated for metadata field")
			}
		}

		// Test triple pointer field
		if pointerProp, ok := result.Properties["pointer"]; ok {
			if pointerProp.OriginalType == "" {
				t.Error("Expected OriginalType to be populated for pointer field")
			}
			if len(pointerProp.TransformPath) == 0 {
				t.Error("Expected TransformPath to be populated for complex pointer type")
			}
		}

		// Test matrix field (slice of slice of map)
		if matrixProp, ok := result.Properties["matrix"]; ok {
			if matrixProp.Type != "array" {
				t.Errorf("Expected matrix type=array, got %s", matrixProp.Type)
			}
			if matrixProp.Items == nil {
				t.Error("Expected matrix to have items")
			}
			if len(matrixProp.TransformPath) == 0 {
				t.Error("Expected TransformPath to be populated for complex matrix type")
			}
		}

		// Verify relationships are detected
		if len(result.Relationships) == 0 {
			t.Error("Expected relationships to be detected")
		}

		// Test JSON serialization of complex result
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("Failed to marshal complex result to JSON: %v", err)
		}

		var parsed map[string]any
		err = json.Unmarshal(jsonBytes, &parsed)
		if err != nil {
			t.Fatalf("Failed to unmarshal complex JSON: %v", err)
		}

		// Verify complex metadata is preserved in JSON
		if properties, ok := parsed["properties"].(map[string]any); ok {
			if idProp, ok := properties["id"].(map[string]any); ok {
				if customTagData, ok := idProp["customTagData"].(map[string]any); ok {
					if validateData, ok := customTagData["validate"].(map[string]any); ok {
						if validateData["required"] != true {
							t.Error("Complex custom tag data not properly serialized")
						}
					}
				}
			}
		}
	})

	t.Run("Performance with deeply nested structures", func(t *testing.T) {
		// Test that processing complex structures doesn't take too long
		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,
		}

		start := time.Now()
		for i := 0; i < 100; i++ {
			_ = router.ExtractSchemaFromType(reflect.TypeOf(DeeplyNestedStruct{}), opts)
		}
		duration := time.Since(start)

		// Should complete 100 iterations in reasonable time (less than 1 second)
		if duration > time.Second {
			t.Errorf("Performance issue: 100 iterations took %v, expected < 1s", duration)
		}

		t.Logf("Performance: 100 iterations completed in %v", duration)
	})
}

// TestRealWorldModels tests schema extraction with realistic project models
func TestRealWorldModels(t *testing.T) {
	t.Run("UserAccountModel with comprehensive options", func(t *testing.T) {
		// Custom tag handlers for real-world validation
		parseValidationRules := func(tag string) any {
			if tag == "" {
				return nil
			}
			rules := make(map[string]any)
			parts := strings.Split(tag, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				switch {
				case part == "required":
					rules["required"] = true
				case part == "email":
					rules["email"] = true
				case part == "phone":
					rules["phone"] = true
				case strings.HasPrefix(part, "min="):
					rules["min"] = strings.TrimPrefix(part, "min=")
				case strings.HasPrefix(part, "max="):
					rules["max"] = strings.TrimPrefix(part, "max=")
				case strings.HasPrefix(part, "oneof="):
					values := strings.Split(strings.TrimPrefix(part, "oneof="), " ")
					rules["oneof"] = values
				}
			}
			return rules
		}

		parseInternalTag := func(tag string) any {
			if tag == "true" {
				return map[string]bool{"internal": true}
			}
			return nil
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,
			SkipAnonymousFields:  boolPtr(false), // Include embedded structs
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": parseValidationRules,
				"internal": parseInternalTag,
			},
			TagPriority:          []string{"json", "bun", "validate"},
			SkipUnexportedFields: boolPtr(true),
			CustomFieldFilter: func(field reflect.StructField) bool {
				// Skip fields marked as internal
				if internal := field.Tag.Get("internal"); internal == "true" {
					return false
				}
				return true
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(UserAccountModel{}), opts)

		// Verify comprehensive model extraction
		if result.Name != "UserAccountModel" {
			t.Errorf("Expected Name=UserAccountModel, got %s", result.Name)
		}

		// Verify complex field types are handled
		expectedComplexFields := map[string]string{
			"id":                   "string", // UUID
			"email":                "string",
			"preferences":          "object", // map[string]interface{}
			"date_of_birth":        "string", // *time.Time
			"last_login_at":        "string", // *time.Time
			"IntegrationBaseModel": "object", // embedded struct
		}

		for fieldName, expectedType := range expectedComplexFields {
			if prop, ok := result.Properties[fieldName]; ok {
				if prop.Type != expectedType {
					t.Errorf("Field %s: expected type=%s, got %s", fieldName, expectedType, prop.Type)
				}
			} else {
				t.Errorf("Expected field '%s' to exist", fieldName)
			}
		}

		// Verify relationships are detected
		expectedRelationships := map[string]string{
			"profile":         "has-one",
			"orders":          "has-many",
			"addresses":       "has-many",
			"payment_methods": "has-many",
			"roles":           "many-to-many",
			"teams":           "many-to-many",
			"notifications":   "has-many",
			"activity_logs":   "has-many",
		}

		for relName, expectedType := range expectedRelationships {
			if rel, ok := result.Relationships[relName]; ok {
				if rel.RelationType != expectedType {
					t.Errorf("Relationship %s: expected type=%s, got %s", relName, expectedType, rel.RelationType)
				}
			} else {
				t.Errorf("Expected relationship '%s' to exist", relName)
			}
		}

		// Verify custom tag processing worked
		if emailProp, ok := result.Properties["email"]; ok {
			if emailProp.CustomTagData == nil {
				t.Error("Expected CustomTagData to be populated for email field")
			} else if validateData, ok := emailProp.CustomTagData["validate"]; ok {
				if rules, ok := validateData.(map[string]any); ok {
					if rules["required"] != true {
						t.Error("Expected email field to have required validation")
					}
					if rules["email"] != true {
						t.Error("Expected email field to have email validation")
					}
				}
			}
		}

		// Verify internal field was filtered out
		if _, exists := result.Properties["internal_debug_data"]; exists {
			t.Error("Expected internal_debug_data field to be filtered out")
		}

		// Verify unexported field was filtered out
		if _, exists := result.Properties["internalField"]; exists {
			t.Error("Expected unexported internalField to be filtered out")
		}

		// Verify password field was filtered out (crud:"-")
		if _, exists := result.Properties["password_hash"]; exists {
			t.Error("Expected password_hash field to be filtered out (crud:\"-\")")
		}
	})

	t.Run("Multiple related models integration", func(t *testing.T) {
		// Test that related models also work correctly
		models := []reflect.Type{
			reflect.TypeOf(UserProfile{}),
			reflect.TypeOf(Address{}),
			reflect.TypeOf(PaymentMethod{}),
			reflect.TypeOf(Role{}),
			reflect.TypeOf(Team{}),
			reflect.TypeOf(Notification{}),
			reflect.TypeOf(ActivityLog{}),
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeTagMetadata:   true,
		}

		for _, modelType := range models {
			result := router.ExtractSchemaFromType(modelType, opts)

			// Each model should have properties
			if len(result.Properties) == 0 {
				t.Errorf("Model %s has no properties", modelType.Name())
			}

			// Each model should have at least an ID field
			foundIDField := false
			for propName := range result.Properties {
				if strings.Contains(strings.ToLower(propName), "id") {
					foundIDField = true
					break
				}
			}
			if !foundIDField {
				t.Errorf("Model %s has no ID field", modelType.Name())
			}

			// Test JSON serialization
			jsonBytes, err := json.Marshal(result)
			if err != nil {
				t.Errorf("Failed to marshal %s to JSON: %v", modelType.Name(), err)
			}

			var parsed map[string]any
			err = json.Unmarshal(jsonBytes, &parsed)
			if err != nil {
				t.Errorf("Failed to unmarshal %s JSON: %v", modelType.Name(), err)
			}
		}
	})
}

// TestIntegrationPerformance tests that new features don't significantly impact performance
func TestIntegrationPerformance(t *testing.T) {
	t.Run("Performance comparison: default vs enhanced options", func(t *testing.T) {
		testType := reflect.TypeOf(UserAccountModel{})

		// Measure default performance
		start := time.Now()
		for i := 0; i < 1000; i++ {
			_ = router.ExtractSchemaFromType(testType)
		}
		defaultDuration := time.Since(start)

		// Measure enhanced options performance
		enhancedOpts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": func(tag string) any {
					if tag == "required" {
						return map[string]bool{"required": true}
					}
					return nil
				},
			},
			TagPriority: []string{"json", "bun", "validate"},
		}

		start = time.Now()
		for i := 0; i < 1000; i++ {
			_ = router.ExtractSchemaFromType(testType, enhancedOpts)
		}
		enhancedDuration := time.Since(start)

		// Calculate performance impact
		performanceImpact := float64(enhancedDuration-defaultDuration) / float64(defaultDuration) * 100

		t.Logf("Default processing: %v", defaultDuration)
		t.Logf("Enhanced processing: %v", enhancedDuration)
		t.Logf("Performance impact: %.2f%%", performanceImpact)

		// Ensure performance impact is acceptable (less than 100% increase)
		if performanceImpact > 100 {
			t.Errorf("Performance impact too high: %.2f%% (should be < 100%%)", performanceImpact)
		}
	})

	t.Run("Memory allocation comparison", func(t *testing.T) {
		testType := reflect.TypeOf(DeeplyNestedStruct{})

		// Test with different option combinations
		testCases := []struct {
			name string
			opts router.ExtractSchemaFromTypeOptions
		}{
			{
				name: "minimal",
				opts: router.ExtractSchemaFromTypeOptions{},
			},
			{
				name: "metadata_only",
				opts: router.ExtractSchemaFromTypeOptions{
					IncludeOriginalNames: true,
					IncludeOriginalTypes: true,
				},
			},
			{
				name: "full_features",
				opts: router.ExtractSchemaFromTypeOptions{
					IncludeOriginalNames: true,
					IncludeOriginalTypes: true,
					IncludeTagMetadata:   true,
					IncludeTypeMetadata:  true,
					CustomTagHandlers: map[string]func(tag string) any{
						"validate": func(tag string) any { return tag },
					},
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Measure processing time
				start := time.Now()
				result := router.ExtractSchemaFromType(testType, tc.opts)
				duration := time.Since(start)

				// Basic validation
				if len(result.Properties) == 0 {
					t.Error("No properties extracted")
				}

				// Log performance metrics
				t.Logf("Configuration '%s': %v", tc.name, duration)

				// Ensure reasonable performance (less than 10ms for single extraction)
				if duration > 10*time.Millisecond {
					t.Errorf("Performance too slow for '%s': %v (should be < 10ms)", tc.name, duration)
				}
			})
		}
	})

	t.Run("Large scale processing", func(t *testing.T) {
		// Test processing many different types
		types := []reflect.Type{
			reflect.TypeOf(UserAccountModel{}),
			reflect.TypeOf(DeeplyNestedStruct{}),
			reflect.TypeOf(UserProfile{}),
			reflect.TypeOf(Address{}),
			reflect.TypeOf(PaymentMethod{}),
			reflect.TypeOf(Role{}),
			reflect.TypeOf(Team{}),
			reflect.TypeOf(Notification{}),
			reflect.TypeOf(ActivityLog{}),
			reflect.TypeOf(CollectionWithRelations{}),
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,
		}

		start := time.Now()
		for i := 0; i < 100; i++ {
			for _, modelType := range types {
				result := router.ExtractSchemaFromType(modelType, opts)
				if len(result.Properties) == 0 {
					t.Errorf("Type %s produced no properties", modelType.Name())
				}
			}
		}
		duration := time.Since(start)

		totalOperations := 100 * len(types)
		avgPerOperation := duration / time.Duration(totalOperations)

		t.Logf("Processed %d operations in %v (avg: %v per operation)", totalOperations, duration, avgPerOperation)

		// Ensure reasonable performance for large scale processing
		if avgPerOperation > 5*time.Millisecond {
			t.Errorf("Average performance too slow: %v per operation (should be < 5ms)", avgPerOperation)
		}
	})
}

// TestIntegrationEdgeCases tests edge cases with integration scenarios
func TestIntegrationEdgeCases(t *testing.T) {
	t.Run("Circular reference handling", func(t *testing.T) {
		// Test structures that could cause circular references
		type CircularNode struct {
			ID        int           `json:"id"`
			RelatedID *int          `json:"related_id,omitempty"`
			Related   *CircularNode `json:"related,omitempty" bun:"rel:belongs-to"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeTypeMetadata:  true,
		}

		// Should not panic or hang
		result := router.ExtractSchemaFromType(reflect.TypeOf(CircularNode{}), opts)

		if len(result.Properties) == 0 {
			t.Error("CircularNode should have properties")
		}
		if len(result.Relationships) == 0 {
			t.Error("CircularNode should have relationships")
		}
	})

	t.Run("Complex generic-like structures", func(t *testing.T) {
		type Container struct {
			Data         map[string][]map[string]*interface{} `json:"data"`
			NestedMaps   map[string]map[string]map[string]any `json:"nested_maps"`
			NestedSlices [][]string                           `json:"nested_slices"`
			MixedNested  []map[string][]*time.Time            `json:"mixed_nested"`
		}

		opts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalTypes: true,
			IncludeTypeMetadata:  true,
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(Container{}), opts)

		// Verify complex nested types are handled
		for fieldName, prop := range result.Properties {
			if prop.OriginalType == "" {
				t.Errorf("Field %s should have OriginalType populated", fieldName)
			}
			if len(prop.TransformPath) == 0 {
				t.Errorf("Field %s should have TransformPath populated", fieldName)
			}
		}
	})

	t.Run("All options combined stress test", func(t *testing.T) {
		// Use the most complex structure with all options enabled
		complexOpts := router.ExtractSchemaFromTypeOptions{
			IncludeOriginalNames: true,
			IncludeOriginalTypes: true,
			IncludeTagMetadata:   true,
			IncludeTypeMetadata:  true,
			CustomTagHandlers: map[string]func(tag string) any{
				"validate": func(tag string) any {
					return map[string]string{"rule": tag}
				},
				"bun": func(tag string) any {
					return map[string]string{"database": tag}
				},
				"json": func(tag string) any {
					return map[string]string{"serialization": tag}
				},
			},
			TagPriority:          []string{"json", "bun", "validate"},
			SkipUnexportedFields: boolPtr(false),
			SkipAnonymousFields:  boolPtr(false),
			CustomFieldFilter: func(field reflect.StructField) bool {
				return !strings.HasPrefix(field.Name, "Skip")
			},
			FieldNameTransformer: func(fieldName string) string {
				return "transformed_" + strings.ToLower(fieldName)
			},
			PropertyTypeMapper: func(t reflect.Type) router.PropertyInfo {
				if t.Kind() == reflect.String {
					return router.PropertyInfo{Type: "custom_string", Format: "enhanced"}
				}
				return router.PropertyInfo{}
			},
		}

		result := router.ExtractSchemaFromType(reflect.TypeOf(UserAccountModel{}), complexOpts)

		// Should not panic and should produce reasonable results
		if len(result.Properties) == 0 {
			t.Error("Complex options should still produce properties")
		}

		// Verify JSON serialization works with all options
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("Failed to marshal complex result: %v", err)
		}

		var parsed map[string]any
		err = json.Unmarshal(jsonBytes, &parsed)
		if err != nil {
			t.Fatalf("Failed to unmarshal complex JSON: %v", err)
		}

		// Verify the result has expected structure
		if _, ok := parsed["properties"]; !ok {
			t.Error("Missing properties in complex result")
		}
		if _, ok := parsed["relationships"]; !ok {
			t.Error("Missing relationships in complex result")
		}
	})
}
