package router

import (
	"reflect"
	"testing"
)

type openapiRelationStubProvider struct {
	descriptor *RelationDescriptor
}

func (s openapiRelationStubProvider) BuildRelationDescriptor(reflect.Type) (*RelationDescriptor, error) {
	return s.descriptor, nil
}

type openapiStubProvider struct {
	metadata ResourceMetadata
}

func (s openapiStubProvider) GetMetadata() ResourceMetadata {
	return s.metadata
}

type openapiRelationResource struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestGenerateOpenAPIIncludesRelationExtension(t *testing.T) {
	resetRelationFilters()

	descriptor := &RelationDescriptor{
		Includes: []string{"books", "books.publisher"},
		Relations: []RelationInfo{
			{
				Name: "books.publisher",
				Filters: []RelationFilter{
					{Field: "country", Operator: "eq", Value: "us"},
				},
			},
		},
		Tree: &RelationNode{
			Name:     "author",
			Display:  "Author",
			TypeName: "router.openapiRelationResource",
			Fields:   []string{"id", "name"},
			Children: map[string]*RelationNode{
				"books": {
					Name:   "books",
					Fields: []string{"title"},
					Children: map[string]*RelationNode{
						"publisher": {
							Name:   "publisher",
							Fields: []string{"name"},
						},
					},
				},
			},
		},
	}

	aggregator := NewMetadataAggregator().
		WithRelationProvider(openapiRelationStubProvider{descriptor: descriptor})

	aggregator.AddProvider(openapiStubProvider{
		metadata: ResourceMetadata{
			Name:         "author",
			PluralName:   "authors",
			ResourceType: reflect.TypeOf(openapiRelationResource{}),
			Schema: SchemaMetadata{
				Name:       "author",
				Properties: map[string]PropertyInfo{},
			},
		},
	})

	aggregator.Compile()

	doc := aggregator.GenerateOpenAPI()
	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("expected components map, got %T", doc["components"])
	}

	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("expected schemas map, got %T", components["schemas"])
	}

	schema, ok := schemas["author"].(map[string]any)
	if !ok {
		t.Fatalf("expected schema map for author, got %T", schemas["author"])
	}

	rawExtension, ok := schema["x-formgen-relations"]
	if !ok {
		t.Fatalf("expected x-formgen-relations extension on schema")
	}

	extension, ok := rawExtension.(map[string]any)
	if !ok {
		t.Fatalf("expected extension to be map[string]any, got %T", rawExtension)
	}

	includes, ok := extension["includes"].([]string)
	if !ok {
		t.Fatalf("expected includes to be []string, got %T", extension["includes"])
	}
	if len(includes) != len(descriptor.Includes) {
		t.Fatalf("expected includes length %d, got %d", len(descriptor.Includes), len(includes))
	}

	tree, ok := extension["tree"].(map[string]any)
	if !ok {
		t.Fatalf("expected tree to be map[string]any, got %T", extension["tree"])
	}
	if tree["name"] != "author" {
		t.Fatalf("expected tree root name 'author', got %v", tree["name"])
	}
}
