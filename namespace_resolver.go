package router

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"strings"
)

var (
	// ErrUnknownNamespace indicates the requested namespace key is not configured.
	ErrUnknownNamespace = errors.New("router: unknown namespace")
	// ErrUnknownRouteKey indicates the requested route key is not configured in the namespace.
	ErrUnknownRouteKey = errors.New("router: unknown route key")
)

// UnknownNamespaceError carries namespace lookup context.
type UnknownNamespaceError struct {
	Namespace string
}

func (e *UnknownNamespaceError) Error() string {
	return fmt.Sprintf("router: namespace %q is not defined", e.Namespace)
}

func (e *UnknownNamespaceError) Unwrap() error {
	return ErrUnknownNamespace
}

// UnknownRouteKeyError carries route key lookup context.
type UnknownRouteKeyError struct {
	Namespace string
	RouteKey  string
}

func (e *UnknownRouteKeyError) Error() string {
	return fmt.Sprintf("router: route key %q is not defined for namespace %q", e.RouteKey, e.Namespace)
}

func (e *UnknownRouteKeyError) Unwrap() error {
	return ErrUnknownRouteKey
}

// NamespaceResolver resolves namespace + route-key mappings into canonical paths.
type NamespaceResolver[NS ~string] struct {
	namespaces map[NS]string
	routes     map[NS]map[string]string
}

// NewNamespaceResolver creates a namespace + route-key resolver.
// Input maps are copied so caller mutations do not affect resolver behavior.
func NewNamespaceResolver[NS ~string](namespaces map[NS]string, routes map[NS]map[string]string) NamespaceResolver[NS] {
	nsCopy := make(map[NS]string, len(namespaces))
	for ns, base := range namespaces {
		nsCopy[ns] = normalizeNamespacePath(base)
	}

	routeCopy := make(map[NS]map[string]string, len(routes))
	for ns, table := range routes {
		if table == nil {
			routeCopy[ns] = nil
			continue
		}
		cloned := make(map[string]string, len(table))
		maps.Copy(cloned, table)
		routeCopy[ns] = cloned
	}

	return NamespaceResolver[NS]{
		namespaces: nsCopy,
		routes:     routeCopy,
	}
}

// Namespace returns the canonical namespace base path.
func (r NamespaceResolver[NS]) Namespace(ns NS) (string, error) {
	base, ok := r.namespaces[ns]
	if !ok {
		return "", &UnknownNamespaceError{Namespace: string(ns)}
	}
	return base, nil
}

// MustNamespace returns Namespace(ns) and panics on error.
func (r NamespaceResolver[NS]) MustNamespace(ns NS) string {
	base, err := r.Namespace(ns)
	if err != nil {
		panic(err)
	}
	return base
}

// Resolve returns the full canonical path for namespace + route key.
func (r NamespaceResolver[NS]) Resolve(ns NS, routeKey string) (string, error) {
	base, err := r.Namespace(ns)
	if err != nil {
		return "", err
	}

	routeKey = strings.TrimSpace(routeKey)
	routeTable, ok := r.routes[ns]
	if !ok {
		return "", &UnknownRouteKeyError{Namespace: string(ns), RouteKey: routeKey}
	}

	relative, ok := routeTable[routeKey]
	if !ok {
		return "", &UnknownRouteKeyError{Namespace: string(ns), RouteKey: routeKey}
	}

	relative = strings.TrimSpace(relative)
	if relative == "" || relative == "/" {
		return base, nil
	}

	return joinCanonicalPath(base, relative), nil
}

// MustResolve returns Resolve(ns, routeKey) and panics on error.
func (r NamespaceResolver[NS]) MustResolve(ns NS, routeKey string) string {
	fullPath, err := r.Resolve(ns, routeKey)
	if err != nil {
		panic(err)
	}
	return fullPath
}

// Relative returns a canonical path relative to the namespace base.
func (r NamespaceResolver[NS]) Relative(ns NS, routeKey string) (string, error) {
	fullPath, err := r.Resolve(ns, routeKey)
	if err != nil {
		return "", err
	}

	base, err := r.Namespace(ns)
	if err != nil {
		return "", err
	}

	relative := strings.TrimPrefix(fullPath, base)
	if relative == "" {
		return "/", nil
	}

	if !strings.HasPrefix(relative, "/") {
		relative = "/" + relative
	}

	return relative, nil
}

// MustRelative returns Relative(ns, routeKey) and panics on error.
func (r NamespaceResolver[NS]) MustRelative(ns NS, routeKey string) string {
	relative, err := r.Relative(ns, routeKey)
	if err != nil {
		panic(err)
	}
	return relative
}

func normalizeNamespacePath(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "/"
	}
	return path.Clean("/" + cleaned)
}

func joinCanonicalPath(base, relative string) string {
	return path.Clean(path.Join(base, relative))
}
