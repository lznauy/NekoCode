package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
)

func TestGlobTool(t *testing.T) {
	td := testutil.SetupTemp(t)
	g := &GlobTool{}

	out, err := g.Execute(testutil.Context(td), map[string]any{"pattern": "*.go", "path": td})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if out.Render() == "" {
		t.Error("expected files matching *.go")
	}

	_, err = g.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}

func TestGlobConfinesPatternsToBase(t *testing.T) {
	base := t.TempDir()
	private := t.TempDir()
	if err := os.WriteFile(filepath.Join(private, "private-name.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(base, private)
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{filepath.Join(rel, "*"), filepath.Join(private, "*"), "../**/*"} {
		_, err := (&GlobTool{}).Execute(testutil.Context(base), map[string]any{"path": base, "pattern": pattern})
		if err == nil {
			t.Errorf("escaping pattern %q accepted", pattern)
		}
	}
	if err := os.Symlink(private, filepath.Join(base, "external")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, pattern := range []string{"external/*", "*/*", "**/*.txt"} {
		out, err := (&GlobTool{}).Execute(testutil.Context(base), map[string]any{"path": base, "pattern": pattern})
		if err == nil && strings.Contains(out.Render(), "private-name.txt") {
			t.Errorf("pattern %q enumerated external files: %s", pattern, out.Render())
		}
	}
}

func TestGlobTraversalPreservesMatchesAndCancellation(t *testing.T) {
	base := testutil.SetupTemp(t)
	for _, pattern := range []string{"*.go", "./*.go", "**/*.go"} {
		out, err := (&GlobTool{}).Execute(testutil.Context(base), map[string]any{"path": base, "pattern": pattern})
		if err != nil || !strings.Contains(out.Render(), filepath.Join(base, "a.go")) {
			t.Fatalf("pattern %q: %v %s", pattern, err, out.Render())
		}
	}
	ctx, cancel := context.WithCancel(testutil.Context(base))
	cancel()
	if _, err := (&GlobTool{}).Execute(ctx, map[string]any{"path": base, "pattern": "**/*"}); err != context.Canceled {
		t.Fatalf("cancellation = %v", err)
	}
	if _, err := (&GlobTool{}).Execute(testutil.Context(base), map[string]any{"path": base, "pattern": "**/["}); err == nil {
		t.Fatal("malformed recursive pattern accepted")
	}
}

func TestRecursiveGlobDirectoryLevels(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.go", "main.go", true}, {"**/*.go", "a/b/main.go", true},
		{"src/**/*.go", "src/main.go", true}, {"src/**/*.go", "src/a/main.go", true},
		{"src/**/*.go", "src2/main.go", false}, {"s*/**/test/*.go", "src/test/main.go", true},
		{"a/**/b/**/c.go", "a/b/c.go", true}, {"a/**/b/**/c.go", "a/x/b/y/c.go", true},
		{"a/**/b/*.go", "a/b/x/c.go", false},
	} {
		if got := matchGlobParts(tc.path, strings.Split(tc.pattern, "/")); got != tc.want {
			t.Errorf("%q matches %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}
