package router

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// // PageContext holds template data
// type PageContextx pongo2.Context

// Update updates this context with the key/value-pairs from another context.
func (c ViewContext) Update(other ViewContext) ViewContext {
	for k, v := range other {
		c[k] = v
	}
	return c
}

func (c ViewContext) asFiberMap() fiber.Map {
	return fiber.Map(c)
}

// SerializeAsContext will return any object as a PageContext instance
func SerializeAsContext(m any) (map[string]any, error) {
	// if ctx, ok := m.(map[string]any); ok {
	// 	return ctx, nil
	// }

	var b []byte
	var err error

	if s, ok := m.(Serializer); ok {
		b, err = s.Serialize()
	} else {
		b, err = json.Marshal(m)
	}

	if err != nil {
		return map[string]any{}, err
	}

	ctx := map[string]any{}
	err = json.Unmarshal(b, &ctx)
	return ctx, err
}
