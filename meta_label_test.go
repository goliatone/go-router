package router_test

import (
	"reflect"
	"testing"

	"github.com/goliatone/go-router"
)

type metadataProviderFunc func() router.ResourceMetadata

func (fn metadataProviderFunc) GetMetadata() router.ResourceMetadata {
	return fn()
}

func TestGetResourceMetadataCapturesLabelField(t *testing.T) {
	type LabelledResource struct {
		ID        int    `json:"id"`
		Name      string `json:"name" crud:"label:name"`
		Slug      string `json:"slug"`
		CreatedAt string `json:"created_at"`
	}

	metadata := router.GetResourceMetadata(reflect.TypeOf(LabelledResource{}))
	if metadata == nil {
		t.Fatalf("GetResourceMetadata returned nil metadata")
	}

	prop, ok := metadata.Schema.Properties["name"]
	if !ok {
		t.Fatalf("expected 'name' property to exist in schema properties: %+v", metadata.Schema.Properties)
	}

	if len(prop.AllTags) == 0 {
		t.Fatalf("expected AllTags to be populated for 'name' when tag metadata is enabled")
	}

	if got := metadata.Schema.LabelField; got != "name" {
		t.Fatalf("expected LabelField to be 'name', got %q", got)
	}
}

func TestMetadataAggregatorEmitsLabelExtension(t *testing.T) {
	type Library struct {
		ID    int    `json:"id"`
		Title string `json:"title" crud:"label:title"`
	}

	resourceMetadata := router.GetResourceMetadata(reflect.TypeOf(Library{}))

	aggregator := router.NewMetadataAggregator()
	aggregator.AddProvider(metadataProviderFunc(func() router.ResourceMetadata {
		return *resourceMetadata
	}))
	aggregator.Compile()

	schemas, ok := aggregator.Components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("expected schemas component to be a map, got %T", aggregator.Components["schemas"])
	}

	schema, ok := schemas[resourceMetadata.Name].(map[string]any)
	if !ok {
		t.Fatalf("expected schema for %s to be a map, got %T", resourceMetadata.Name, schemas[resourceMetadata.Name])
	}

	value, ok := schema["x-formgen-label-field"]
	if !ok {
		t.Fatalf("expected x-formgen-label-field extension to be present, schema: %+v", schema)
	}

	if value != "title" {
		t.Fatalf("expected x-formgen-label-field to be 'title', got %v", value)
	}
}
