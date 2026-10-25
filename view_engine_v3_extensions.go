package router

import (
	"fmt"
	"strings"

	"github.com/flosch/pongo2/v7"
)

// FiberV3ViewExtensions configures filters and tags on the private template sets
// returned by DefaultFiberV3ViewEngine and InitializeFiberV3ViewEngine. Register
// before Load/Render. Later registrations invalidate the compiled snapshot;
// existing renders finish with their original snapshot. Reload retains them.
// Extension implementations must be safe for concurrent template execution.
type FiberV3ViewExtensions interface {
	RegisterFilter(string, pongo2.FilterFunction) error
	RegisterTag(string, pongo2.TagParser) error
}

var _ FiberV3ViewExtensions = (*fiberV3DjangoViewEngine)(nil)

func (e *fiberV3DjangoViewEngine) RegisterFilter(name string, fn pongo2.FilterFunction) error {
	e.Mutex.Lock()
	defer e.Mutex.Unlock()
	if strings.TrimSpace(name) == "" || fn == nil {
		return fmt.Errorf("fiber v3 views: filter name and function are required")
	}
	if pongo2.BuiltinFilterExists(name) || e.filters[name] != nil {
		return fmt.Errorf("fiber v3 views: filter %q is already registered", name)
	}
	if e.filters == nil {
		e.filters = make(map[string]pongo2.FilterFunction)
	}
	// Filters can box values from another runtime even though their signature
	// uses v7. Apply the same boundary as helpers before the evaluator sees the
	// result, preserving native v7 safety flags rather than guessing v6 flags.
	e.filters[name] = func(in, param *pongo2.Value) (*pongo2.Value, error) {
		result, err := fn(in, param)
		if err != nil {
			return nil, err
		}
		if _, err := fiberV3ViewValue(result, "filter "+name+" result"); err != nil {
			return nil, err
		}
		return result, nil
	}
	e.Loaded = false
	return nil
}

func (e *fiberV3DjangoViewEngine) RegisterTag(name string, parser pongo2.TagParser) error {
	e.Mutex.Lock()
	defer e.Mutex.Unlock()
	if strings.TrimSpace(name) == "" || parser == nil {
		return fmt.Errorf("fiber v3 views: tag name and parser are required")
	}
	if pongo2.BuiltinTagExists(name) || e.tags[name] != nil {
		return fmt.Errorf("fiber v3 views: tag %q is already registered", name)
	}
	if e.tags == nil {
		e.tags = make(map[string]pongo2.TagParser)
	}
	e.tags[name] = parser
	e.Loaded = false
	return nil
}
