package router

import (
	"io/fs"
	"path/filepath"
	"runtime"
)

// AbsFromCaller resolves rel relative to the caller's source file.
// If rel is absolute, it is returned as-is. If runtime.Caller fails,
// filepath.Clean(rel) is returned instead.
//
// Example:
//
//	assetsDir := router.AbsFromCaller("testdata/static")
func AbsFromCaller(rel string, skip ...int) string {
	if filepath.IsAbs(rel) {
		return rel
	}

	totalSkip := 1
	if len(skip) > 0 {
		totalSkip += skip[0]
	}

	_, file, _, ok := runtime.Caller(totalSkip)
	if !ok {
		return filepath.Clean(rel)
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), rel))
}

// SubFS scopes base to subdir if present, otherwise returns base.
// It mirrors autoSubFS behavior by normalizing paths and ignoring
// missing subdirectories.
//
// Example:
//
//	assetsFS, _ := router.SubFS(embeddedAssets, "assets")
func SubFS(base fs.FS, subdir string) (fs.FS, error) {
	clean := NormalizePath(subdir)
	if clean == "" || clean == "." {
		return base, nil
	}

	if _, err := fs.Stat(base, clean); err != nil {
		return base, nil
	}

	return fs.Sub(base, clean)
}

// MustSubFS is a convenience wrapper around SubFS that panics on error.
func MustSubFS(base fs.FS, subdir string) fs.FS {
	sub, err := SubFS(base, subdir)
	if err != nil {
		panic(err)
	}
	return sub
}

// PathParam returns a parameter segment (e.g., ":id").
func PathParam(name string) string {
	return ":" + name
}

// ConstrainedPathParam returns a parameter segment with a constraint.
// Note: constraint syntax is Fiber-specific; httprouter treats it as part of the param name.
func ConstrainedPathParam(name, constraint string) string {
	if constraint == "" {
		return ":" + name
	}
	return ":" + name + "<" + constraint + ">"
}
