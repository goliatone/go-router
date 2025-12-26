package router_test

import (
	"embed"
	"io/fs"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goliatone/go-router"
)

//go:embed testdata/static/*
var staticFS embed.FS

func TestAbsFromCaller(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed in test")

	expected := filepath.Clean(filepath.Join(filepath.Dir(file), "testdata/static"))
	actual := router.AbsFromCaller("testdata/static")

	assert.Equal(t, expected, actual)
}

func TestSubFS(t *testing.T) {
	sub, err := router.SubFS(staticFS, "testdata/static")
	require.NoError(t, err)

	data, err := fs.ReadFile(sub, "index.html")
	require.NoError(t, err)
	assert.Contains(t, string(data), "static index")
}

func TestMustSubFS(t *testing.T) {
	sub := router.MustSubFS(staticFS, "testdata/static")

	data, err := fs.ReadFile(sub, "index.html")
	require.NoError(t, err)
	assert.Contains(t, string(data), "static index")
}
