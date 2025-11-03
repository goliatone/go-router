package router

import (
	"reflect"
	"testing"
)

type uiSpecAuthor struct {
	ID   int64  `bun:"id,pk" json:"id"`
	Name string `bun:"name" json:"full_name"`
}

type uiSpecEditor struct {
	ID   int64  `bun:"id,pk" json:"id"`
	Name string `bun:"name" json:"name"`
}

type uiSpecTag struct {
	ID    int64  `bun:"id,pk" json:"id"`
	Label string `bun:"label" json:"label"`
}

type uiSpecArticle struct {
	ID       int64         `bun:"id,pk" json:"id"`
	TenantID int64         `bun:"tenant_id" json:"tenant_id"`
	AuthorID int64         `bun:"author_id" json:"author_id"`
	Author   *uiSpecAuthor `bun:"rel:belongs-to,join:author_id=id" crud:"endpoint:/api/authors,labelField:full_name,valueField:id,param:include=profile,dynamicParam:tenant_id={{field:tenant_id}},inverse:articles" json:"author,omitempty"`
	EditorID int64         `bun:"editor_id" json:"editor_id"`
	Editor   *uiSpecEditor `bun:"rel:belongs-to,join:editor_id=id" json:"editor,omitempty"`
	Tags     []uiSpecTag   `bun:"rel:has-many,join:id=article_id" crud:"inverse:article,endpoint:/api/tags,labelField:label,valueField:id,param:limit=50,param:order=label asc,param:select=id,label,param:format=options,mode:search,searchParam:q,submitAs:json" json:"tags,omitempty"`
}

type staticProvider struct {
	metadata ResourceMetadata
}

func (p staticProvider) GetMetadata() ResourceMetadata {
	return p.metadata
}

func stringMapValue(t *testing.T, value any, key string) string {
	t.Helper()
	switch typed := value.(type) {
	case map[string]string:
		if v, ok := typed[key]; ok {
			return v
		}
	case map[string]any:
		if v, ok := typed[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	t.Fatalf("expected %v to contain string value for %q", value, key)
	return ""
}

func TestMetadataAggregator_RelationshipExtensions(t *testing.T) {
	articleMD := GetResourceMetadata(reflect.TypeOf(uiSpecArticle{}))
	authorMD := GetResourceMetadata(reflect.TypeOf(uiSpecAuthor{}))
	tagMD := GetResourceMetadata(reflect.TypeOf(uiSpecTag{}))

	agg := NewMetadataAggregator()
	agg.AddProviders(
		staticProvider{metadata: *articleMD},
		staticProvider{metadata: *authorMD},
		staticProvider{metadata: *tagMD},
	)
	agg.Compile()

	schemaAny, exists := agg.Schemas[articleMD.Name]
	if !exists {
		t.Fatalf("expected schema for %s to be compiled", articleMD.Name)
	}

	schema, ok := schemaAny.(map[string]any)
	if !ok {
		t.Fatalf("schema for %s had unexpected type %T", articleMD.Name, schemaAny)
	}

	propsAny := schema["properties"]
	props, ok := propsAny.(map[string]any)
	if !ok {
		t.Fatalf("schema properties had unexpected type %T", propsAny)
	}

	authorAny := props["author"]
	authorProp, ok := authorAny.(map[string]any)
	if !ok {
		t.Fatalf("author property had unexpected type %T", authorAny)
	}

	relExtAny := authorProp["x-relationships"]
	relExt, ok := relExtAny.(map[string]any)
	if !ok {
		t.Fatalf("author property missing x-relationships extension")
	}

	if gotType := relExt["type"]; gotType != "belongsTo" {
		t.Fatalf("expected author relationship type belongsTo, got %v", gotType)
	}
	if target := relExt["target"]; target != "#/components/schemas/"+authorMD.Name {
		t.Fatalf("expected author target #/components/schemas/%s, got %v", authorMD.Name, target)
	}
	if fk := relExt["foreignKey"]; fk != "author_id" {
		t.Fatalf("expected author foreignKey author_id, got %v", fk)
	}
	if source := relExt["sourceField"]; source != "author_id" {
		t.Fatalf("expected author sourceField author_id, got %v", source)
	}
	if inverse := relExt["inverse"]; inverse != "articles" {
		t.Fatalf("expected author inverse articles, got %v", inverse)
	}

	endpointAny := authorProp["x-endpoint"]
	endpoint, ok := endpointAny.(map[string]any)
	if !ok {
		t.Fatalf("author property missing x-endpoint extension")
	}
	if url := endpoint["url"]; url != "/api/authors" {
		t.Fatalf("expected author endpoint url /api/authors, got %v", url)
	}
	if method := endpoint["method"]; method != "GET" {
		t.Fatalf("expected author endpoint method GET, got %v", method)
	}
	if val := stringMapValue(t, endpoint["params"], "include"); val != "profile" {
		t.Fatalf("expected author endpoint params include=profile, got %v", endpoint["params"])
	}
	if val := stringMapValue(t, endpoint["dynamicParams"], "tenant_id"); val != "{{field:tenant_id}}" {
		t.Fatalf("expected author endpoint dynamic tenant param, got %v", endpoint["dynamicParams"])
	}

	authorAliasAny := props["author_id"]
	authorAlias, ok := authorAliasAny.(map[string]any)
	if !ok {
		t.Fatalf("author_id property had unexpected type %T", authorAliasAny)
	}

	aliasRelAny := authorAlias["x-relationships"]
	aliasRel, ok := aliasRelAny.(map[string]any)
	if !ok {
		t.Fatalf("author_id property missing x-relationships extension")
	}
	if _, hasSource := aliasRel["sourceField"]; hasSource {
		t.Fatalf("author_id relationship should not include sourceField, got %v", aliasRel)
	}
	if target := aliasRel["target"]; target != "#/components/schemas/"+authorMD.Name {
		t.Fatalf("expected author_id target #/components/schemas/%s, got %v", authorMD.Name, target)
	}

	aliasEndpointAny := authorAlias["x-endpoint"]
	aliasEndpoint, ok := aliasEndpointAny.(map[string]any)
	if !ok {
		t.Fatalf("author_id property missing x-endpoint extension")
	}
	if url := aliasEndpoint["url"]; url != "/api/authors" {
		t.Fatalf("expected author_id endpoint url /api/authors, got %v", url)
	}
	if val := stringMapValue(t, aliasEndpoint["params"], "include"); val != "profile" {
		t.Fatalf("expected author_id endpoint params include=profile, got %v", aliasEndpoint["params"])
	}

	tagsAny := props["tags"]
	tagsProp, ok := tagsAny.(map[string]any)
	if !ok {
		t.Fatalf("tags property had unexpected type %T", tagsAny)
	}

	if propType := tagsProp["type"]; propType != "array" {
		t.Fatalf("expected tags property type array, got %v", propType)
	}
	items, ok := tagsProp["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected tags property to include items with $ref, got %v", tagsProp["items"])
	}
	if ref := items["$ref"]; ref != "#/components/schemas/ui-spec-tag" {
		t.Fatalf("expected tags items $ref to target ui-spec-tag, got %v", ref)
	}

	tagsRelAny := tagsProp["x-relationships"]
	tagsRel, ok := tagsRelAny.(map[string]any)
	if !ok {
		t.Fatalf("expected tags x-relationships extension, got %v", tagsRelAny)
	}
	if relType := tagsRel["type"]; relType != "hasMany" {
		t.Fatalf("expected tags relationship type hasMany, got %v", relType)
	}
	if target := tagsRel["target"]; target != "#/components/schemas/ui-spec-tag" {
		t.Fatalf("expected tags relationship target ui-spec-tag, got %v", target)
	}
	if cardinality := tagsRel["cardinality"]; cardinality != "many" {
		t.Fatalf("expected tags cardinality many, got %v", cardinality)
	}
	if inverse := tagsRel["inverse"]; inverse != "article" {
		t.Fatalf("expected tags inverse article, got %v", inverse)
	}

	tagsEndpointAny := tagsProp["x-endpoint"]
	tagsEndpoint, ok := tagsEndpointAny.(map[string]any)
	if !ok {
		t.Fatalf("expected tags x-endpoint extension, got %v", tagsEndpointAny)
	}
	if url := tagsEndpoint["url"]; url != "/api/tags" {
		t.Fatalf("expected tags endpoint url /api/tags, got %v", url)
	}
	if mode := tagsEndpoint["mode"]; mode != "search" {
		t.Fatalf("expected tags endpoint mode search, got %v", mode)
	}
	if searchParam := tagsEndpoint["searchParam"]; searchParam != "q" {
		t.Fatalf("expected tags endpoint searchParam q, got %v", searchParam)
	}
	if submitAs := tagsEndpoint["submitAs"]; submitAs != "json" {
		t.Fatalf("expected tags endpoint submitAs json, got %v", submitAs)
	}
	if limit := stringMapValue(t, tagsEndpoint["params"], "limit"); limit != "50" {
		t.Fatalf("expected tags endpoint limit param 50, got %v", limit)
	}
	if order := stringMapValue(t, tagsEndpoint["params"], "order"); order != "label asc" {
		t.Fatalf("expected tags endpoint order param 'label asc', got %v", order)
	}
	if selectParam := stringMapValue(t, tagsEndpoint["params"], "select"); selectParam != "id,label" {
		t.Fatalf("expected tags endpoint select param 'id,label', got %v", selectParam)
	}
}

func TestMetadataAggregator_UISchemaOptionsOverrides(t *testing.T) {
	articleMD := GetResourceMetadata(reflect.TypeOf(uiSpecArticle{}))
	authorMD := GetResourceMetadata(reflect.TypeOf(uiSpecAuthor{}))
	editorMD := GetResourceMetadata(reflect.TypeOf(uiSpecEditor{}))
	tagMD := GetResourceMetadata(reflect.TypeOf(uiSpecTag{}))

	agg := NewMetadataAggregator().WithUISchemaOptions(UISchemaOptions{
		EndpointOverrides: map[string]map[string]*EndpointHint{
			articleMD.Name: {
				"author": {
					URL:        "/override/authors",
					Method:     "post",
					LabelField: "override",
				},
			},
		},
		EndpointDefaults: func(resource *ResourceMetadata, relationName string, rel *RelationshipInfo) *EndpointHint {
			if relationName == "editor" {
				return &EndpointHint{URL: "/api/editors", Method: "GET", LabelField: "name", ValueField: "id"}
			}
			return nil
		},
	})

	agg.AddProviders(
		staticProvider{metadata: *articleMD},
		staticProvider{metadata: *authorMD},
		staticProvider{metadata: *editorMD},
		staticProvider{metadata: *tagMD},
	)
	agg.Compile()

	schemaAny := agg.Schemas[articleMD.Name]
	schema := schemaAny.(map[string]any)
	props := schema["properties"].(map[string]any)

	authorProp := props["author"].(map[string]any)
	authorEndpoint := authorProp["x-endpoint"].(map[string]any)
	if url := authorEndpoint["url"]; url != "/override/authors" {
		t.Fatalf("expected override url /override/authors, got %v", url)
	}
	if method := authorEndpoint["method"]; method != "POST" {
		t.Fatalf("expected override method POST, got %v", method)
	}
	if label := authorEndpoint["labelField"]; label != "override" {
		t.Fatalf("expected override labelField override, got %v", label)
	}

	editorProp := props["editor"].(map[string]any)
	editorEndpoint := editorProp["x-endpoint"].(map[string]any)
	if url := editorEndpoint["url"]; url != "/api/editors" {
		t.Fatalf("expected default url /api/editors, got %v", url)
	}
	if method := editorEndpoint["method"]; method != "GET" {
		t.Fatalf("expected default method GET, got %v", method)
	}
	if label := editorEndpoint["labelField"]; label != "name" {
		t.Fatalf("expected default labelField name, got %v", label)
	}
}

func TestMetadataAggregator_RelationFilterRemovesRelationship(t *testing.T) {
	articleMD := GetResourceMetadata(reflect.TypeOf(uiSpecArticle{}))
	authorMD := GetResourceMetadata(reflect.TypeOf(uiSpecAuthor{}))
	tagMD := GetResourceMetadata(reflect.TypeOf(uiSpecTag{}))

	agg := NewMetadataAggregator().WithUISchemaOptions(UISchemaOptions{
		RelationFilters: []RelationshipInfoFilter{
			func(resource *ResourceMetadata, relationName string, rel *RelationshipInfo) *RelationshipInfo {
				if relationName == "editor" {
					return nil
				}
				return rel
			},
		},
	})

	agg.AddProviders(staticProvider{metadata: *articleMD}, staticProvider{metadata: *authorMD}, staticProvider{metadata: *tagMD})
	agg.Compile()

	schemaAny := agg.Schemas[articleMD.Name]
	schema := schemaAny.(map[string]any)
	props := schema["properties"].(map[string]any)

	editorProp := props["editor"].(map[string]any)
	if _, ok := editorProp["x-relationships"]; ok {
		t.Fatalf("expected editor relationship to be filtered out, found %v", editorProp["x-relationships"])
	}
	if _, ok := editorProp["x-endpoint"]; ok {
		t.Fatalf("expected editor endpoint to be absent after filter, got %v", editorProp["x-endpoint"])
	}

	editorAlias := props["editor_id"].(map[string]any)
	if _, ok := editorAlias["x-relationships"]; ok {
		t.Fatalf("expected editor_id relationship to be filtered out, got %v", editorAlias["x-relationships"])
	}
}

func TestMetadataAggregator_DefaultEndpointFallback(t *testing.T) {
	articleMD := GetResourceMetadata(reflect.TypeOf(uiSpecArticle{}))
	authorMD := GetResourceMetadata(reflect.TypeOf(uiSpecAuthor{}))
	editorMD := GetResourceMetadata(reflect.TypeOf(uiSpecEditor{}))
	tagMD := GetResourceMetadata(reflect.TypeOf(uiSpecTag{}))

	agg := NewMetadataAggregator()
	agg.AddProviders(
		staticProvider{metadata: *articleMD},
		staticProvider{metadata: *authorMD},
		staticProvider{metadata: *editorMD},
		staticProvider{metadata: *tagMD},
	)
	agg.Compile()

	schemaAny := agg.Schemas[articleMD.Name]
	schema, ok := schemaAny.(map[string]any)
	if !ok {
		t.Fatalf("expected schema to be map, got %T", schemaAny)
	}

	propsAny := schema["properties"]
	props, ok := propsAny.(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", propsAny)
	}

	editorPropAny := props["editor"]
	editorProp, ok := editorPropAny.(map[string]any)
	if !ok {
		t.Fatalf("expected editor property map, got %T", editorPropAny)
	}

	endpointAny, ok := editorProp["x-endpoint"]
	if !ok {
		t.Fatalf("expected fallback x-endpoint for editor relation")
	}
	endpoint, ok := endpointAny.(map[string]any)
	if !ok {
		t.Fatalf("expected editor x-endpoint to be map, got %T", endpointAny)
	}

	if url := endpoint["url"]; url != "/api/ui-spec-editors" {
		t.Fatalf("expected fallback endpoint url /api/ui-spec-editors, got %v", url)
	}
	if method := endpoint["method"]; method != "GET" {
		t.Fatalf("expected fallback endpoint method GET, got %v", method)
	}
	if label := endpoint["labelField"]; label != "name" {
		t.Fatalf("expected fallback labelField name, got %v", label)
	}
	if value := endpoint["valueField"]; value != "id" {
		t.Fatalf("expected fallback valueField id, got %v", value)
	}
	if limit := stringMapValue(t, endpoint["params"], "limit"); limit != "50" {
		t.Fatalf("expected fallback limit 50, got %v", limit)
	}
	if order := stringMapValue(t, endpoint["params"], "order"); order != "name asc" {
		t.Fatalf("expected fallback order 'name asc', got %v", order)
	}
	if selectParam := stringMapValue(t, endpoint["params"], "select"); selectParam != "id,name" {
		t.Fatalf("expected fallback select 'id,name', got %v", selectParam)
	}
	if mode := endpoint["mode"]; mode != "search" {
		t.Fatalf("expected fallback mode search, got %v", mode)
	}
	if search := endpoint["searchParam"]; search != "q" {
		t.Fatalf("expected fallback searchParam q, got %v", search)
	}
	if submit := endpoint["submitAs"]; submit != "json" {
		t.Fatalf("expected fallback submitAs json, got %v", submit)
	}
}
