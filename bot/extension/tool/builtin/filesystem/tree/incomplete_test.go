package tree

import (
	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTreeReportsUnreadableSubdirectory(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	os.Mkdir(blocked, 0700)
	os.WriteFile(filepath.Join(blocked, "exists"), []byte("data"), 0600)
	os.Chmod(blocked, 0000)
	defer os.Chmod(blocked, 0700)
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("environment bypasses directory permission checks")
	}
	out, err := (&TreeTool{}).Execute(testutil.Context(dir), map[string]any{"path": dir})
	if err == nil || !strings.Contains(out.Text, "incomplete") || !strings.Contains(out.Text, "blocked") {
		t.Fatalf("read failure hidden: %q %v", out.Text, err)
	}
}

func TestTreeExplainsHiddenOnlyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".config"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := (&TreeTool{}).Execute(testutil.Context(dir), map[string]any{"path": dir})
	if err != nil || !strings.Contains(out.Text, "no visible entries") || !strings.Contains(out.Text, "list") {
		t.Fatalf("hidden directory guidance: %q %v", out.Text, err)
	}
}

func TestTreeOnlyMarksActualOmissions(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		out, err := (&TreeTool{}).Execute(testutil.Context(dir), map[string]any{"path": dir, "limit": float64(1)})
		if err != nil || strings.Contains(out.Text, "omitted") != want {
			t.Fatalf("tree: %q %v", out.Text, err)
		}
	}
	check(false)
	if err := os.WriteFile(filepath.Join(child, "nested"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	check(true)
}
