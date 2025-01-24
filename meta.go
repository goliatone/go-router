package router

func NewRouteDefinition() *RouteDefinition {
	return &RouteDefinition{
		Tags:       make([]string, 0),
		Parameters: make([]Parameter, 0),
		Responses:  make([]Response, 0),
	}
}

// Ensure RouteDefinition implements RouteInfo

func (r *RouteDefinition) SetName(n string) RouteInfo {
	r.Name = n
	if r.onSetName != nil {
		r.onSetName(n)
	}
	return r
}

func (r *RouteDefinition) SetDescription(d string) RouteInfo {
	r.Description = d
	return r
}

func (r *RouteDefinition) SetSummary(s string) RouteInfo {
	r.Summary = s
	return r
}

func (r *RouteDefinition) AddTags(t ...string) RouteInfo {
	r.Tags = append(r.Tags, t...)
	return r
}

func (r *RouteDefinition) AddParameter(name, in string, required bool, schema map[string]any) RouteInfo {
	r.Parameters = append(r.Parameters, Parameter{
		Name:     name,
		In:       in,
		Required: required,
		Schema:   schema,
	})
	return r
}

func (r *RouteDefinition) SetRequestBody(desc string, required bool, content map[string]any) RouteInfo {
	r.RequestBody = &RequestBody{
		Description: desc,
		Required:    required,
		Content:     content,
	}
	return r
}

func (r *RouteDefinition) AddResponse(code int, desc string, content map[string]any) RouteInfo {
	r.Responses = append(r.Responses, Response{
		Code:        code,
		Description: desc,
		Content:     content,
	})
	return r
}
