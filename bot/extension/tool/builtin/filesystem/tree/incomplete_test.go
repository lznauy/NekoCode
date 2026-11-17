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
