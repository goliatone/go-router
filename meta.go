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
	// Additional fields like security, deprecated, operationId could be added
}

type RouteDefinition struct {
	Method    HTTPMethod
	Path      string
	name      string
	Operation Operation
	Handlers  []NamedHandler
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
