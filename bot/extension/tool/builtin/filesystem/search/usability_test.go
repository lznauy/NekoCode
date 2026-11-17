package search

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
)

type unreadableSubdirFS struct{ fs.FS }

func (f unreadableSubdirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "blocked" {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrPermission}
	}
	return fs.ReadDir(f.FS, name)
}

func TestRecursiveGlobReportsUnreadableSubdirectory(t *testing.T) {
	files := unreadableSubdirFS{fstest.MapFS{
		"blocked/hidden.go": &fstest.MapFile{Data: []byte("hidden")},
		"visible.go":        &fstest.MapFile{Data: []byte("visible")},
	}}
	_, err := globRecursive(t.Context(), files, []string{"**", "*.go"})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("incomplete search must report permission error: %v", err)
	}
}

func TestGlobOrdinaryPatternReportsUnreadableDirectory(t *testing.T) {
	files := unreadableSubdirFS{fstest.MapFS{"blocked/hidden.go": &fstest.MapFile{Data: []byte("hidden")}}}
	_, err := globNonRecursive(t.Context(), files, "blocked/*.go")
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("ordinary glob swallowed permission failure: %v", err)
	}
}

func TestGlobOrdinaryPatternDoesNotHideEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "outside")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	out, err := (&GlobTool{}).Execute(testutil.Context(dir), map[string]any{"path": dir, "pattern": "outside/*"})
	if err == nil || strings.Contains(out.Text, "No matching files") {
		t.Fatalf("failed traversal reported as empty search: %q %v", out.Text, err)
	}
}

func TestGlobOrdinaryNonmatchesAndCancellation(t *testing.T) {
	dir := testutil.SetupTemp(t)
	for _, pattern := range []string{"missing/*.go", "a.go/*", "no-such-file.go"} {
		out, err := (&GlobTool{}).Execute(testutil.Context(dir), map[string]any{"path": dir, "pattern": pattern})
		if err != nil || out.Text != "No matching files found" {
			t.Errorf("ordinary nonmatch %q: %q %v", pattern, out.Text, err)
		}
	}
	ctx, cancel := context.WithCancel(testutil.Context(dir))
	cancel()
	if _, err := (&GlobTool{}).Execute(ctx, map[string]any{"path": dir, "pattern": "*.go"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation swallowed: %v", err)
	}
}

func TestGlobRejectsInvalidPathType(t *testing.T) {
	dir := testutil.SetupTemp(t)
	for _, value := range []any{nil, 42, []any{dir}} {
		if _, err := (&GlobTool{}).Execute(testutil.Context(dir), map[string]any{"pattern": "*.go", "path": value}); err == nil {
			t.Fatalf("invalid path accepted: %#v", value)
		}
	}
}

func TestGrepNumericContext(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("before\nneedle\nafter\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, counts := range []map[string]any{{"context": 1.0}, {"before": 1, "after": 1}} {
		args := map[string]any{"pattern": "needle", "path": dir}
		for key, value := range counts {
			args[key] = value
		}
		out, err := (&GrepTool{}).Execute(testutil.Context(dir), args)
		if err != nil || !strings.Contains(out.Text, "before") || !strings.Contains(out.Text, "after") {
			t.Fatalf("numeric context ignored: %q %v", out.Text, err)
		}
	}
	for _, args := range []map[string]any{
		{"context": -1}, {"before": 1.5}, {"after": "1"}, {"context": nil},
		{"context": 1, "before": 2}, {"context": 1, "context_lines": "-C2"},
	} {
		args["pattern"], args["path"] = "needle", dir
		if _, err := (&GrepTool{}).Execute(testutil.Context(dir), args); err == nil {
			t.Fatalf("invalid context accepted: %#v", args)
		}
	}
}

func TestGlobScopedIgnoresUnrelatedUnreadableDirectory(t *testing.T) {
	files := unreadableSubdirFS{fstest.MapFS{
		"src/main.go":            &fstest.MapFile{Data: []byte("package main")},
		"blocked/irrelevant.txt": &fstest.MapFile{Data: []byte("irrelevant")},
	}}
	matches, err := globRecursive(t.Context(), files, []string{"src", "**", "*.go"})
	if err != nil || len(matches) != 1 || matches[0] != "src/main.go" {
		t.Fatalf("unrelated directory broke scoped glob: matches=%v err=%v", matches, err)
	}
}
