package router

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestPrepareStaticFilesystemWithCustomRoot(t *testing.T) {
	br := &BaseRouter{logger: &defaultLogger{}}
	cfg := Static{
		FS: fstest.MapFS{
			"uploads/file.txt": &fstest.MapFile{Data: []byte("ok")},
		},
		Root: "uploads",
	}

	fsys, err := br.prepareStaticFilesystem("/files", cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	content, err := fs.ReadFile(fsys, "file.txt")
	if err != nil {
		t.Fatalf("expected to read file from sub filesystem, got %v", err)
	}

	if string(content) != "ok" {
		t.Fatalf("unexpected content %q", content)
	}
}

func TestPrepareStaticFilesystemWithMissingRootFails(t *testing.T) {
	br := &BaseRouter{logger: &defaultLogger{}}
	cfg := Static{
		FS:   fstest.MapFS{},
		Root: "missing",
	}

	if _, err := br.prepareStaticFilesystem("/files", cfg); err == nil {
		t.Fatalf("expected error when root is missing")
	}
}

func TestPrepareStaticFilesystemWithFileRootFails(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "asset.txt")

	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	br := &BaseRouter{logger: &defaultLogger{}}
	cfg := Static{Root: filePath}

	if _, err := br.prepareStaticFilesystem("/files", cfg); err == nil {
		t.Fatalf("expected error when root points to file")
	}
}

func TestMergeStaticConfigPreservesDefaults(t *testing.T) {
	cfg := mergeStaticConfig(Static{Root: "./public", Index: "index.html"})
	if cfg.Root != "./public" {
		t.Fatalf("expected root to remain ./public, got %q", cfg.Root)
	}
	if cfg.Index != "index.html" {
		t.Fatalf("expected default index.html, got %q", cfg.Index)
	}
}

func TestDetectConsecutiveDuplicateSegment(t *testing.T) {
	if seg := detectConsecutiveDuplicateSegment("public/uploads/uploads"); seg != "uploads" {
		t.Fatalf("expected uploads duplicate, got %q", seg)
	}

	if seg := detectConsecutiveDuplicateSegment("public/assets/img"); seg != "" {
		t.Fatalf("expected no duplicate, got %q", seg)
	}
}
