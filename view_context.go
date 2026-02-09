package router

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/gofiber/fiber/v2"
)

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

	var ctx map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()

	if err := dec.Decode(&ctx); err != nil {
		return map[string]any{}, err
	}

	if err := ensureJSONEOF(dec); err != nil {
		return map[string]any{}, err
	}

	if ctx == nil {
		return map[string]any{}, nil
	}

	return normalizeJSONNumbers(ctx).(map[string]any), nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var trailing any
	err := dec.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func normalizeJSONNumbers(v any) any {
	switch val := v.(type) {
	case map[string]any:
		for k, item := range val {
			val[k] = normalizeJSONNumbers(item)
		}
		return val
	case []any:
		for i, item := range val {
			val[i] = normalizeJSONNumbers(item)
		}
		return val
	case json.Number:
		raw := val.String()
		if !strings.ContainsAny(raw, ".eE") {
			if n, err := val.Int64(); err == nil {
				return n
			}
		}

		if n, err := val.Float64(); err == nil {
			return n
		}

		return raw
	default:
		return v
	}
}
