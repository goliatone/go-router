package router

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/ettle/strcase"
)

// RelationFilter represents an allowed filter on a relation path.
type RelationFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// RelationInfo captures relation-level filter metadata for a given include path.
type RelationInfo struct {
	Name    string           `json:"name"`
	Filters []RelationFilter `json:"filters,omitempty"`
}

// RelationNode describes an entity and its nested relations for metadata purposes.
type RelationNode struct {
	Name      string                   `json:"name"`
	Display   string                   `json:"display,omitempty"`
	TypeName  string                   `json:"typeName,omitempty"`
	Fields    []string                 `json:"fields,omitempty"`
	Aliases   []string                 `json:"aliases,omitempty"`
	Operators []string                 `json:"operators,omitempty"`
	Children  map[string]*RelationNode `json:"children,omitempty"`
}

// RelationDescriptor bundles relation tree data with flattened include metadata.
type RelationDescriptor struct {
	Tree      *RelationNode  `json:"tree,omitempty"`
	Includes  []string       `json:"includes,omitempty"`
	Relations []RelationInfo `json:"relations,omitempty"`
}

// RelationMetadataProvider generates relation metadata for the supplied resource type.
type RelationMetadataProvider interface {
	BuildRelationDescriptor(resourceType reflect.Type) (*RelationDescriptor, error)
}

// RelationFilterFunc allows frameworks to mutate relation metadata prior to publication.
type RelationFilterFunc func(resourceType reflect.Type, descriptor *RelationDescriptor) *RelationDescriptor

var (
	relationFilterMu sync.RWMutex
	relationFilters  []RelationFilterFunc
)

// RegisterRelationFilter appends a filter function executed in registration order.
func RegisterRelationFilter(filter RelationFilterFunc) {
	if filter == nil {
		return
	}
	relationFilterMu.Lock()
	defer relationFilterMu.Unlock()
	relationFilters = append(relationFilters, filter)
}

// ApplyRelationFilters runs all registered filters against the descriptor.
func ApplyRelationFilters(resourceType reflect.Type, descriptor *RelationDescriptor) *RelationDescriptor {
	relationFilterMu.RLock()
	defer relationFilterMu.RUnlock()

	result := descriptor
	for _, filter := range relationFilters {
		if filter == nil {
			continue
		}
		result = filter(resourceType, result)
		if result == nil {
			break
		}
	}
	return result
}

// resetRelationFilters clears the global filter chain (used in tests).
func resetRelationFilters() {
	relationFilterMu.Lock()
	defer relationFilterMu.Unlock()
	relationFilters = nil
}

// DefaultRelationProvider reflects Bun CRUD tags to build relation metadata.
type DefaultRelationProvider struct {
	fieldResolver func(reflect.Type) map[string]string
}

// NewDefaultRelationProvider constructs a provider using native field extraction.
func NewDefaultRelationProvider() *DefaultRelationProvider {
	return &DefaultRelationProvider{
		fieldResolver: defaultFieldResolver,
	}
}

// BuildRelationDescriptor implements RelationMetadataProvider.
func (p *DefaultRelationProvider) BuildRelationDescriptor(resourceType reflect.Type) (*RelationDescriptor, error) {
	if resourceType == nil {
		return nil, errors.New("relation metadata requires a non-nil type")
	}

	base := indirectType(resourceType)
	if base.Kind() != reflect.Struct {
		return nil, errors.New("relation metadata requires a struct type")
	}

	visited := make(map[reflect.Type]bool)
	root := p.buildNode(base, canonicalResourceName(base), visited)
	descriptor := &RelationDescriptor{Tree: root}
	if root != nil {
		descriptor.Includes = collectIncludePaths(root, nil)
	}
	return descriptor, nil
}

func (p *DefaultRelationProvider) buildNode(t reflect.Type, name string, visited map[reflect.Type]bool) *RelationNode {
	base := indirectType(t)
	if base.Kind() != reflect.Struct {
		return nil
	}

	node := &RelationNode{
		Name:     name,
		Display:  displayName(base, name),
		TypeName: base.String(),
		Fields:   p.collectFields(base),
		Children: make(map[string]*RelationNode),
	}

	if visited[base] {
		return node
	}

	visited[base] = true

	for i := 0; i < base.NumField(); i++ {
		field := base.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Tag.Get(TAG_CRUD) == "-" {
			continue
		}

		bunTag := field.Tag.Get(TAG_BUN)
		if bunTag == "" || !strings.Contains(bunTag, "rel:") {
			continue
		}

		childType := field.Type
		for childType.Kind() == reflect.Ptr || childType.Kind() == reflect.Slice || childType.Kind() == reflect.Array {
			childType = childType.Elem()
		}

		childName, aliases := relationRequestNames(field)
		childNode := p.buildNode(childType, childName, visited)
		if childNode == nil {
			continue
		}

		childNode.Aliases = appendUnique(childNode.Aliases, aliases...)
		node.Children[strings.ToLower(childName)] = childNode
		for _, alias := range aliases {
			node.Children[strings.ToLower(alias)] = childNode
		}
	}

	delete(visited, base)
	return node
}

func (p *DefaultRelationProvider) collectFields(t reflect.Type) []string {
	resolve := p.fieldResolver
	if resolve == nil {
		resolve = defaultFieldResolver
	}

	fields := resolve(t)
	if len(fields) == 0 {
		return nil
	}

	names := make([]string, 0, len(fields))
	for name := range fields {
		if name != "" {
			names = append(names, name)
		}
	}

	sort.Strings(names)
	return names
}

func defaultFieldResolver(t reflect.Type) map[string]string {
	base := indirectType(t)
	if base.Kind() != reflect.Struct {
		return nil
	}

	fields := make(map[string]string)

	for i := 0; i < base.NumField(); i++ {
		field := base.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Tag.Get(TAG_CRUD) == "-" {
			continue
		}

		if field.Anonymous {
			embedded := defaultFieldResolver(field.Type)
			for k, v := range embedded {
				fields[k] = v
			}
			continue
		}

		jsonTag := field.Tag.Get(TAG_JSON)
		name := strings.TrimSpace(strings.Split(jsonTag, ",")[0])
		if name == "" || name == "-" {
			name = strcase.ToSnake(field.Name)
		}
		if name == "" {
			name = field.Name
		}

		fields[name] = name
	}

	return fields
}

func relationRequestNames(field reflect.StructField) (string, []string) {
	jsonTag := field.Tag.Get(TAG_JSON)
	alias := strings.TrimSpace(strings.Split(jsonTag, ",")[0])

	if alias == "" || alias == "-" {
		alias = strcase.ToSnake(field.Name)
	}

	aliases := []string{}
	if !strings.EqualFold(alias, field.Name) {
		aliases = append(aliases, field.Name)
	}

	return alias, aliases
}

func canonicalResourceName(t reflect.Type) string {
	name := t.Name()
	if name == "" {
		return "resource"
	}
	return strings.ToLower(name)
}

func displayName(t reflect.Type, fallback string) string {
	if fallback != "" {
		return strcase.ToCase(fallback, strcase.TitleCase, ' ')
	}
	return strcase.ToCase(t.Name(), strcase.TitleCase, ' ')
}

func indirectType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func collectIncludePaths(node *RelationNode, prefix []string) []string {
	if node == nil {
		return nil
	}
	var includes []string

	for key, child := range node.Children {
		if child == nil {
			continue
		}
		path := append(prefix, child.Name)
		if child.Name == "" {
			// Fallback to map key if explicit name missing.
			path[len(path)-1] = key
		}
		pathValue := strings.Join(path, ".")
		includes = append(includes, pathValue)
		includes = append(includes, collectIncludePaths(child, path)...)
	}

	sort.Strings(includes)
	return includes
}

func appendUnique(target []string, values ...string) []string {
	existing := make(map[string]struct{}, len(target))
	for _, v := range target {
		existing[strings.ToLower(v)] = struct{}{}
	}

	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := existing[strings.ToLower(v)]; ok {
			continue
		}
		target = append(target, v)
		existing[strings.ToLower(v)] = struct{}{}
	}
	return target
}
