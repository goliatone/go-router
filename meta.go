package router

func NewRouteDefinition() *RouteDefinition {
	return &RouteDefinition{
		metadata: &RouteMetadata{
			Tags:       make([]string, 0),
			Parameters: make([]Parameter, 0),
			Responses:  make([]Response, 0),
		},
	}
}

// TODO: Either use lowercase or functions
type RouteDefinition struct {
	metadata *RouteMetadata
}

// Ensure RouteDefinition implements RouteInfo

func (r *RouteDefinition) SetName(n string) RouteInfo {
	r.metadata.Name = n
	return r
}

func (r *RouteDefinition) SetDescription(d string) RouteInfo {
	r.metadata.Description = d
	return r
}

func (r *RouteDefinition) SetSummary(s string) RouteInfo {
	r.metadata.Summary = s
	return r
}

func (r *RouteDefinition) AddTags(t ...string) RouteInfo {
	r.metadata.Tags = append(r.metadata.Tags, t...)
	return r
}

func (r *RouteDefinition) AddParameter(name, in string, required bool, schema any) RouteInfo {
	r.metadata.Parameters = append(r.metadata.Parameters, Parameter{
		Name:     name,
		In:       in,
		Required: required,
		Schema:   schema.(map[string]any),
	})
	return r
}

func (r *RouteDefinition) SetRequestBody(desc string, required bool, content map[string]any) RouteInfo {
	r.metadata.RequestBody = &RequestBody{
		Description: desc,
		Required:    required,
		Content:     content,
	}
	return r
}

func (r *RouteDefinition) AddResponse(code int, desc string, content map[string]any) RouteInfo {
	r.metadata.Responses = append(r.metadata.Responses, Response{
		Code:        code,
		Description: desc,
		Content:     content,
	})
	return r
}
