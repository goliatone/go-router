package router

type Parameter struct {
	Name     string
	In       string
	Required bool
	Schema   any // Could be a JSON schema snippet
}

type RequestBody struct {
	Description string
	Content     map[string]any
	Required    bool
}

type Response struct {
	Code        int
	Description string
	Content     map[string]any
}

type Operation struct {
	Summary     string
	Description string
	Tags        []string
	Parameters  []Parameter
	RequestBody *RequestBody
	Responses   []Response
	//TODO: Additional fields like security, deprecated, operationId could be added
}

// TODO: Either use lowercase or functions
type RouteDefinition struct {
	Method    HTTPMethod
	Path      string
	name      string
	Operation Operation
	Handlers  []NamedHandler
}

func (r *RouteDefinition) FromRouteDefinition(r2 *RouteDefinition) RouteInfo {

	if r2.name != "" {
		r.name = r2.name
	}

	if r2.Operation.Description != "" {
		r.Description(r2.Operation.Description)
	}

	if r2.Operation.Summary != "" {
		r.Summary(r2.Operation.Summary)
	}

	if len(r2.Operation.Tags) > 0 {
		r.Tags(r2.Operation.Tags...)
	}

	for _, p := range r2.Operation.Parameters {
		r.AddParameter(p.Name, p.In, p.Required, p.Schema)
	}

	if r2.Operation.RequestBody != nil {
		r.SetRequestBody(
			r2.Operation.RequestBody.Description,
			r2.Operation.RequestBody.Required,
			r2.Operation.RequestBody.Content,
		)
	}

	for _, resp := range r2.Operation.Responses {
		r.AddResponse(resp.Code, resp.Description, resp.Content)
	}

	return r
}

// Ensure RouteDefinition implements RouteInfo

func (r *RouteDefinition) Name(n string) RouteInfo {
	r.name = n
	return r
}

func (r *RouteDefinition) Description(d string) RouteInfo {
	r.Operation.Description = d
	return r
}

func (r *RouteDefinition) Summary(s string) RouteInfo {
	r.Operation.Summary = s
	return r
}

func (r *RouteDefinition) Tags(t ...string) RouteInfo {
	r.Operation.Tags = append(r.Operation.Tags, t...)
	return r
}

func (r *RouteDefinition) AddParameter(name, in string, required bool, schema any) RouteInfo {
	r.Operation.Parameters = append(r.Operation.Parameters, Parameter{
		Name:     name,
		In:       in,
		Required: required,
		Schema:   schema,
	})
	return r
}

func (r *RouteDefinition) SetRequestBody(desc string, required bool, content map[string]any) RouteInfo {
	r.Operation.RequestBody = &RequestBody{
		Description: desc,
		Required:    required,
		Content:     content,
	}
	return r
}

func (r *RouteDefinition) AddResponse(code int, desc string, content map[string]any) RouteInfo {
	r.Operation.Responses = append(r.Operation.Responses, Response{
		Code:        code,
		Description: desc,
		Content:     content,
	})
	return r
}
