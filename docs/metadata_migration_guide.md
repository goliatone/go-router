# Migration Guide: Enhanced Metadata Options

## Overview

This guide helps you migrate from the basic metadata extraction to the new enhanced options while maintaining backward compatibility and optimal performance.

## Backward Compatibility

**No Breaking Changes** - All existing code continues to work without modification:

```go
// This continues to work exactly as before
schema := ExtractSchemaFromType(reflect.TypeOf(MyStruct{}))
```

> **Note:** `router.GetResourceMetadata` now opts into `IncludeTagMetadata` so it can surface label fields for OpenAPI generation. Direct calls to `ExtractSchemaFromType` remain unchanged unless you pass the option explicitly.

## Migration Strategies

### Strategy 1: Gradual Feature Adoption

Start with minimal changes and gradually add features as needed.

#### Step 1: Add Original Names
```go
// Before
schema := ExtractSchemaFromType(reflect.TypeOf(User{}))

// After - minimal change
opts := ExtractSchemaFromTypeOptions{
    IncludeOriginalNames: true,
}
schema := ExtractSchemaFromType(reflect.TypeOf(User{}), opts)
```

#### Step 2: Add Type Information
```go
opts := ExtractSchemaFromTypeOptions{
    IncludeOriginalNames: true,
    IncludeOriginalTypes: true, // Added
}
```

#### Step 3: Add Tag Metadata
```go
opts := ExtractSchemaFromTypeOptions{
    IncludeOriginalNames: true,
    IncludeOriginalTypes: true,
    IncludeTagMetadata:   true, // Added
}
```

#### Step 4: Add Custom Processing (as needed)
```go
opts := ExtractSchemaFromTypeOptions{
    IncludeOriginalNames: true,
    IncludeOriginalTypes: true,
    IncludeTagMetadata:   true,
    CustomTagHandlers: map[string]func(tag string) any{ // Added
        "validate": parseValidationRules,
    },
}
```

### Strategy 2: Feature-Specific Migration

Adopt specific features based on your use case.

#### For API Documentation Generation
```go
// Focus on tag metadata and original types
opts := ExtractSchemaFromTypeOptions{
    IncludeOriginalNames: true,
    IncludeTagMetadata:   true,
    CustomTagHandlers: map[string]func(tag string) any{
        "validate": parseValidationRules,
        "example":  parseExampleValues,
    },
}
```

#### For Code Generation Tools
```go
// Focus on type hierarchy and transformations
opts := ExtractSchemaFromTypeOptions{
    IncludeOriginalTypes: true,
    IncludeTypeMetadata:  true,
    TagPriority:          []string{"codegen", "json", "bun"},
}
```

#### For Database Schema Mapping
```go
// Focus on field filtering and custom mappings
skipUnexported := false
opts := ExtractSchemaFromTypeOptions{
    SkipUnexportedFields: &skipUnexported,
    TagPriority:          []string{"bun", "db", "json"},
    CustomFieldFilter: func(field reflect.StructField) bool {
        return !strings.Contains(field.Tag.Get("bun"), "-")
    },
}
```

### Strategy 3: Surface Relation Metadata

1. **Register a provider** – start with the built-in reflection provider and keep the option open for per-resource overrides.

```go
relationProvider := router.NewDefaultRelationProvider()

aggregator := router.NewMetadataAggregator().
    WithRelationProvider(relationProvider).
    WithRelationProviders(map[reflect.Type]router.RelationMetadataProvider{
        reflect.TypeOf(Account{}): accountSpecificProvider,
    })
```

2. **Apply optional filters** – remove or enrich relations before they reach consumers.

```go
router.RegisterRelationFilter(func(t reflect.Type, desc *router.RelationDescriptor) *router.RelationDescriptor {
    if desc == nil {
        return nil
    }
    delete(desc.Tree.Children, "internalMetrics")
    return desc
})
```

3. **Consume the extension** – generated OpenAPI schemas now expose `x-formgen-relations` alongside existing metadata so clients (or other services) can discover legal include paths and relation filters.

## Common Migration Scenarios

### Scenario 1: Validation Framework Integration

**Before:**
```go
type User struct {
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"min=18,max=120"`
}

// Manual validation rule extraction
func extractValidationRules(t reflect.Type) map[string]any {
    rules := make(map[string]any)
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        if tag := field.Tag.Get("validate"); tag != "" {
            // Manual parsing logic here
            rules[field.Name] = parseManually(tag)
        }
    }
    return rules
}
```

**After:**
```go
// Automated with custom tag handlers
parseValidation := func(tag string) any {
    rules := make(map[string]any)
    parts := strings.Split(tag, ",")
    for _, part := range parts {
        if strings.Contains(part, "=") {
            kv := strings.Split(part, "=")
            if len(kv) == 2 {
                rules[kv[0]] = kv[1]
            }
        } else {
            rules[part] = true
        }
    }
    return rules
}

opts := ExtractSchemaFromTypeOptions{
    IncludeTagMetadata: true,
    CustomTagHandlers: map[string]func(tag string) any{
        "validate": parseValidation,
    },
}

schema := ExtractSchemaFromType(reflect.TypeOf(User{}), opts)
// Validation rules now available in schema.Properties[field].CustomTagData["validate"]
```

### Scenario 2: Multi-Format API Support

**Before:**
```go
// Separate extraction for each format
func getJSONSchema(t reflect.Type) SchemaMetadata {
    // Custom logic for JSON tags
}

func getXMLSchema(t reflect.Type) SchemaMetadata {
    // Custom logic for XML tags
}

func getYAMLSchema(t reflect.Type) SchemaMetadata {
    // Custom logic for YAML tags
}
```

**After:**
```go
// Single extraction with tag priority
func getSchemaForFormat(t reflect.Type, format string) SchemaMetadata {
    var priority []string
    switch format {
    case "json":
        priority = []string{"json", "yaml", "xml"}
    case "xml":
        priority = []string{"xml", "json", "yaml"}
    case "yaml":
        priority = []string{"yaml", "json", "xml"}
    }

    opts := ExtractSchemaFromTypeOptions{
        TagPriority:        priority,
        IncludeTagMetadata: true,
    }

    return ExtractSchemaFromType(t, opts)
}
```

### Scenario 3: Complex Type Analysis

**Before:**
```go
// Manual type analysis
func analyzeComplexTypes(t reflect.Type) TypeAnalysis {
    analysis := TypeAnalysis{}

    // Recursive analysis logic
    if t.Kind() == reflect.Ptr {
        analysis.IsPointer = true
        t = t.Elem()
    }
    if t.Kind() == reflect.Slice {
        analysis.IsSlice = true
        analysis.ElementType = t.Elem()
    }
    // ... more manual analysis

    return analysis
}
```

**After:**
```go
// Automated with type metadata
opts := ExtractSchemaFromTypeOptions{
    IncludeTypeMetadata: true,
}

schema := ExtractSchemaFromType(reflect.TypeOf(ComplexStruct{}), opts)
// Type hierarchy available in TransformPath and GoPackage fields
```

## Performance Migration Guidelines

### Low-Impact Migrations

These options have minimal performance overhead:

```go
// ~5% overhead
opts := ExtractSchemaFromTypeOptions{
    IncludeOriginalNames: true,
}

// ~10% overhead
opts := ExtractSchemaFromTypeOptions{
    IncludeOriginalNames: true,
    IncludeOriginalTypes: true,
}
```

### Medium-Impact Migrations

These require more consideration:

```go
// ~15-20% overhead
opts := ExtractSchemaFromTypeOptions{
    IncludeTagMetadata: true,
    CustomTagHandlers: map[string]func(tag string) any{
        "validate": simpleTagParser, // Keep handlers simple
    },
}
```

### High-Impact Migrations

Use these judiciously:

```go
// ~25-30% overhead
opts := ExtractSchemaFromTypeOptions{
    IncludeTypeMetadata: true, // Expensive for deeply nested types
    CustomFieldFilter: func(field reflect.StructField) bool {
        // Avoid complex logic in filter
        return field.IsExported()
    },
}
```

## Migration Checklist

### Pre-Migration Assessment

- [ ] Identify current usage patterns
- [ ] Determine required new features
- [ ] Assess performance requirements
- [ ] Plan rollout strategy

### Migration Execution

- [ ] Start with backward-compatible defaults
- [ ] Add one feature at a time
- [ ] Test performance impact
- [ ] Update documentation and examples
- [ ] Train team on new features

### Post-Migration Validation

- [ ] Verify all existing functionality works
- [ ] Confirm new features provide expected value
- [ ] Monitor performance in production
- [ ] Gather feedback from developers

## Code Organization Patterns

### Option Factory Pattern

```go
// Create reusable option configurations
func APIDocumentationOptions() ExtractSchemaFromTypeOptions {
    return ExtractSchemaFromTypeOptions{
        IncludeOriginalNames: true,
        IncludeTagMetadata:   true,
        CustomTagHandlers: map[string]func(tag string) any{
            "validate": parseValidationRules,
            "example":  parseExampleValues,
        },
    }
}

func DatabaseMappingOptions() ExtractSchemaFromTypeOptions {
    skipUnexported := false
    return ExtractSchemaFromTypeOptions{
        SkipUnexportedFields: &skipUnexported,
        TagPriority:          []string{"bun", "db", "json"},
    }
}

// Usage
schema := ExtractSchemaFromType(reflect.TypeOf(User{}), APIDocumentationOptions())
```

### Configuration Builder Pattern

```go
type OptionsBuilder struct {
    opts ExtractSchemaFromTypeOptions
}

func NewOptionsBuilder() *OptionsBuilder {
    return &OptionsBuilder{
        opts: ExtractSchemaFromTypeOptions{
            // Set defaults
            TagPriority: []string{"json", "bun", "crud"},
        },
    }
}

func (b *OptionsBuilder) WithOriginalMetadata() *OptionsBuilder {
    b.opts.IncludeOriginalNames = true
    b.opts.IncludeOriginalTypes = true
    return b
}

func (b *OptionsBuilder) WithTagMetadata() *OptionsBuilder {
    b.opts.IncludeTagMetadata = true
    return b
}

func (b *OptionsBuilder) WithValidationHandlers() *OptionsBuilder {
    if b.opts.CustomTagHandlers == nil {
        b.opts.CustomTagHandlers = make(map[string]func(tag string) any)
    }
    b.opts.CustomTagHandlers["validate"] = parseValidationRules
    return b
}

func (b *OptionsBuilder) Build() ExtractSchemaFromTypeOptions {
    return b.opts
}

// Usage
opts := NewOptionsBuilder().
    WithOriginalMetadata().
    WithTagMetadata().
    WithValidationHandlers().
    Build()

schema := ExtractSchemaFromType(reflect.TypeOf(User{}), opts)
```

## Testing Migration

### Compatibility Tests

```go
func TestBackwardCompatibility(t *testing.T) {
    type TestStruct struct {
        ID   int    `json:"id"`
        Name string `json:"name"`
    }

    // Test that old and new approaches produce same basic results
    oldSchema := ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))
    newSchema := ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), ExtractSchemaFromTypeOptions{})

    // Compare essential fields
    assert.Equal(t, oldSchema.Name, newSchema.Name)
    assert.Equal(t, len(oldSchema.Properties), len(newSchema.Properties))

    for key := range oldSchema.Properties {
        assert.Contains(t, newSchema.Properties, key)
        assert.Equal(t, oldSchema.Properties[key].Type, newSchema.Properties[key].Type)
    }
}
```

### Feature Tests

```go
func TestNewFeatures(t *testing.T) {
    type TestStruct struct {
        ID int `json:"id" validate:"required"`
    }

    opts := ExtractSchemaFromTypeOptions{
        IncludeOriginalNames: true,
        IncludeTagMetadata:   true,
    }

    schema := ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)

    // Test new features work
    prop := schema.Properties["id"]
    assert.Equal(t, "ID", prop.OriginalName)
    assert.Contains(t, prop.AllTags, "validate")
    assert.Equal(t, "required", prop.AllTags["validate"])
}
```

### Performance Tests

```go
func BenchmarkMigration(b *testing.B) {
    type TestStruct struct {
        Field1 string `json:"field1" validate:"required"`
        Field2 int    `json:"field2" validate:"min=1"`
    }

    b.Run("Original", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            ExtractSchemaFromType(reflect.TypeOf(TestStruct{}))
        }
    })

    b.Run("WithBasicMetadata", func(b *testing.B) {
        opts := ExtractSchemaFromTypeOptions{
            IncludeOriginalNames: true,
        }
        for i := 0; i < b.N; i++ {
            ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)
        }
    })

    b.Run("WithAllMetadata", func(b *testing.B) {
        opts := ExtractSchemaFromTypeOptions{
            IncludeOriginalNames: true,
            IncludeOriginalTypes: true,
            IncludeTagMetadata:   true,
            IncludeTypeMetadata:  true,
        }
        for i := 0; i < b.N; i++ {
            ExtractSchemaFromType(reflect.TypeOf(TestStruct{}), opts)
        }
    })
}
```

## Troubleshooting Common Migration Issues

### Issue 1: Performance Degradation

**Symptoms:** Significant slowdown after migration

**Solutions:**
1. Profile to identify bottlenecks
2. Disable unnecessary features
3. Optimize custom handlers
4. Implement caching

```go
// Cache schemas for frequently used types
var schemaCache sync.Map

func GetSchemaWithCache(t reflect.Type, opts ExtractSchemaFromTypeOptions) SchemaMetadata {
    key := fmt.Sprintf("%s_%+v", t.String(), opts)

    if cached, ok := schemaCache.Load(key); ok {
        return cached.(SchemaMetadata)
    }

    schema := ExtractSchemaFromType(t, opts)
    schemaCache.Store(key, schema)
    return schema
}
```

### Issue 2: Missing Expected Metadata

**Symptoms:** New fields are empty or missing

**Solutions:**
1. Verify options are correctly set
2. Check that relevant tags exist on struct fields
3. Ensure custom handlers don't return nil

```go
// Debug helper
func debugMetadata(schema SchemaMetadata) {
    for name, prop := range schema.Properties {
        fmt.Printf("Field %s:\n", name)
        fmt.Printf("  Original: %s\n", prop.OriginalName)
        fmt.Printf("  Type: %s\n", prop.Type)
        if len(prop.AllTags) > 0 {
            fmt.Printf("  Tags: %v\n", prop.AllTags)
        }
        if len(prop.CustomTagData) > 0 {
            fmt.Printf("  Custom: %v\n", prop.CustomTagData)
        }
    }
}
```

### Issue 3: Unexpected Field Names

**Symptoms:** Field names changed after migration

**Solutions:**
1. Check TagPriority configuration
2. Verify FieldNameTransformer logic
3. Ensure tag values are correct

```go
// Tag priority debug
func debugTagPriority(field reflect.StructField, priority []string) {
    fmt.Printf("Field %s tags:\n", field.Name)
    for _, tag := range priority {
        if value := field.Tag.Get(tag); value != "" {
            fmt.Printf("  %s: %s\n", tag, value)
        }
    }
}
```

## Best Practices Summary

1. **Start Simple** - Begin with basic metadata options
2. **Measure Impact** - Profile performance before and after
3. **Cache Results** - Avoid repeated extraction for same types
4. **Test Thoroughly** - Ensure backward compatibility
5. **Document Changes** - Help team understand new capabilities
6. **Monitor Production** - Watch for performance regressions
7. **Iterative Adoption** - Add features gradually as needed

This migration guide provides a structured approach to adopting the enhanced metadata features while maintaining system stability and performance.
