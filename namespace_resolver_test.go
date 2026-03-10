package router_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goliatone/go-router"
)

type testSurface string

const (
	surfacePublic    testSurface = "public"
	surfaceProtected testSurface = "protected"
	surfaceRoot      testSurface = "root"
)

func TestNamespaceResolver_Namespace_HappyPath(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfacePublic:    "public/api/v1/",
			surfaceProtected: "/api/v1",
			surfaceRoot:      "",
		},
		nil,
	)

	publicNS, err := resolver.Namespace(surfacePublic)
	require.NoError(t, err)
	assert.Equal(t, "/public/api/v1", publicNS)

	protectedNS, err := resolver.Namespace(surfaceProtected)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1", protectedNS)

	rootNS, err := resolver.Namespace(surfaceRoot)
	require.NoError(t, err)
	assert.Equal(t, "/", rootNS)
}

func TestNamespaceResolver_Namespace_UnknownNamespace(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfacePublic: "/public/api/v1",
		},
		nil,
	)

	_, err := resolver.Namespace(testSurface("missing"))
	require.Error(t, err)
	assert.ErrorIs(t, err, router.ErrUnknownNamespace)

	var typedErr *router.UnknownNamespaceError
	require.True(t, errors.As(err, &typedErr))
	assert.Equal(t, "missing", typedErr.Namespace)
}

func TestNamespaceResolver_Resolve_HappyPath(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfaceProtected: "/api/v1",
		},
		map[testSurface]map[string]string{
			surfaceProtected: {
				"wizard.session.create": "/wizard/sessions",
				"wizard.session.dot":    "wizard/./sessions/:id",
			},
		},
	)

	resolved, err := resolver.Resolve(surfaceProtected, "wizard.session.create")
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/wizard/sessions", resolved)

	dotResolved, err := resolver.Resolve(surfaceProtected, "wizard.session.dot")
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/wizard/sessions/:id", dotResolved)
}

func TestNamespaceResolver_Resolve_UnknownRouteKey(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfaceProtected: "/api/v1",
		},
		map[testSurface]map[string]string{
			surfaceProtected: {},
		},
	)

	_, err := resolver.Resolve(surfaceProtected, "  missing.key  ")
	require.Error(t, err)
	assert.ErrorIs(t, err, router.ErrUnknownRouteKey)

	var typedErr *router.UnknownRouteKeyError
	require.True(t, errors.As(err, &typedErr))
	assert.Equal(t, string(surfaceProtected), typedErr.Namespace)
	assert.Equal(t, "missing.key", typedErr.RouteKey)
}

func TestNamespaceResolver_Resolve_MissingRouteTableIsUnknownRouteKey(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfaceProtected: "/api/v1",
		},
		map[testSurface]map[string]string{},
	)

	_, err := resolver.Resolve(surfaceProtected, "wizard.session.create")
	require.Error(t, err)
	assert.ErrorIs(t, err, router.ErrUnknownRouteKey)
}

func TestNamespaceResolver_Relative_HappyPath(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfaceProtected: "/api/v1",
		},
		map[testSurface]map[string]string{
			surfaceProtected: {
				"wizard.session.create": "/wizard/sessions",
			},
		},
	)

	relative, err := resolver.Relative(surfaceProtected, "wizard.session.create")
	require.NoError(t, err)
	assert.Equal(t, "/wizard/sessions", relative)
}

func TestNamespaceResolver_Relative_RootBehavior(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfaceProtected: "/api/v1/",
			surfaceRoot:      "/",
		},
		map[testSurface]map[string]string{
			surfaceProtected: {
				"root.slash": "/",
				"root.blank": "   ",
			},
			surfaceRoot: {
				"health": "/health",
				"root":   "/",
			},
		},
	)

	relativeSlash, err := resolver.Relative(surfaceProtected, "root.slash")
	require.NoError(t, err)
	assert.Equal(t, "/", relativeSlash)

	relativeBlank, err := resolver.Relative(surfaceProtected, "root.blank")
	require.NoError(t, err)
	assert.Equal(t, "/", relativeBlank)

	relativeRootNamespace, err := resolver.Relative(surfaceRoot, "health")
	require.NoError(t, err)
	assert.Equal(t, "/health", relativeRootNamespace)

	relativeRootOnly, err := resolver.Relative(surfaceRoot, "root")
	require.NoError(t, err)
	assert.Equal(t, "/", relativeRootOnly)
}

func TestNamespaceResolver_PathNormalizationEdgeCases(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfaceProtected: "///api//v1///",
		},
		map[testSurface]map[string]string{
			surfaceProtected: {
				"slashes":     "//wizard//sessions//:id//",
				"dot":         "wizard/./sessions/../sessions",
				"dotdot":      "../v2/users",
				"mixed-clean": "/wizard/./sessions//latest/",
			},
		},
	)

	ns, err := resolver.Namespace(surfaceProtected)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1", ns)

	slashes, err := resolver.Resolve(surfaceProtected, "slashes")
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/wizard/sessions/:id", slashes)

	dot, err := resolver.Resolve(surfaceProtected, "dot")
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/wizard/sessions", dot)

	dotdot, err := resolver.Resolve(surfaceProtected, "dotdot")
	require.NoError(t, err)
	assert.Equal(t, "/api/v2/users", dotdot)

	mixedClean, err := resolver.Resolve(surfaceProtected, "mixed-clean")
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/wizard/sessions/latest", mixedClean)
}

func TestNamespaceResolver_MustVariantsPanic(t *testing.T) {
	resolver := router.NewNamespaceResolver(
		map[testSurface]string{
			surfacePublic: "/public/api/v1",
		},
		map[testSurface]map[string]string{
			surfacePublic: {
				"health": "/health",
			},
		},
	)

	assert.Equal(t, "/public/api/v1", resolver.MustNamespace(surfacePublic))
	assert.Equal(t, "/public/api/v1/health", resolver.MustResolve(surfacePublic, "health"))
	assert.Equal(t, "/health", resolver.MustRelative(surfacePublic, "health"))

	assert.Panics(t, func() {
		resolver.MustNamespace(testSurface("missing"))
	})
	assert.Panics(t, func() {
		resolver.MustResolve(surfacePublic, "missing")
	})
	assert.Panics(t, func() {
		resolver.MustRelative(surfacePublic, "missing")
	})
}

func TestNamespaceResolver_ConstructorDefensiveCopy(t *testing.T) {
	namespaces := map[testSurface]string{
		surfacePublic: "/public/api/v1",
	}

	routes := map[testSurface]map[string]string{
		surfacePublic: {
			"health": "/health",
		},
	}

	resolver := router.NewNamespaceResolver(namespaces, routes)

	namespaces[surfacePublic] = "/changed"
	routes[surfacePublic]["health"] = "/changed"
	routes[surfacePublic]["new"] = "/new"

	base, err := resolver.Namespace(surfacePublic)
	require.NoError(t, err)
	assert.Equal(t, "/public/api/v1", base)

	health, err := resolver.Resolve(surfacePublic, "health")
	require.NoError(t, err)
	assert.Equal(t, "/public/api/v1/health", health)

	_, err = resolver.Resolve(surfacePublic, "new")
	require.Error(t, err)
	assert.ErrorIs(t, err, router.ErrUnknownRouteKey)
}
