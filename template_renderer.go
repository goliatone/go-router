package router

import "io"

// TemplateRenderer is an optional context capability for rendering configured
// templates without committing the live response.
type TemplateRenderer interface {
	RenderToWriter(w io.Writer, name string, bind any, layouts ...string) error
	RenderToBytes(name string, bind any, layouts ...string) ([]byte, error)
}

// AsTemplateRenderer returns the non-committing template render capability when
// a context implementation supports it.
func AsTemplateRenderer(c Context) (TemplateRenderer, bool) {
	if c == nil {
		return nil, false
	}
	renderer, ok := c.(TemplateRenderer)
	return renderer, ok
}
