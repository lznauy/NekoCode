package read

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
)

func TestReadOptionalRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("line\n", 1100)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args        map[string]any
		first, last string
	}{
		{map[string]any{}, "1:line", "500:line"},
		{map[string]any{"startLine": 501}, "501:line", "1000:line"},
		{map[string]any{"endLine": 2}, "1:line", "2:line"},
	} {
		tc.args["path"] = path
		out, err := (&ReadTool{}).Execute(testutil.Context(dir), tc.args)
		if err != nil || !strings.Contains(out.Text, "\n"+tc.first+"\n") || !strings.Contains(out.Text, "\n"+tc.last+"\n") {
			t.Fatalf("default range: %v %q", err, out.Text)
		}
	}
}

func TestReadDistinguishesFinishedRangeFromPageLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines")
	if err := os.WriteFile(path, []byte(strings.Repeat("x\n", 600)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		end  int
		want string
	}{{500, "requested range complete"}, {501, "next startLine=501"}} {
		out, err := (&ReadTool{}).Execute(testutil.Context(dir), map[string]any{"path": path, "endLine": tc.end})
		if err != nil || !strings.Contains(out.Text, tc.want) {
			t.Fatalf("endLine %d: missing %q, err=%v", tc.end, tc.want, err)
		}
	}
	_, err := (&ReadTool{}).Execute(testutil.Context(dir), map[string]any{"path": path, "startLine": 999})
	if err == nil || !strings.Contains(err.Error(), "choose a line from 1 to") {
		t.Fatalf("missing recovery range: %v", err)
	}
}

func TestReadNamedWorkspaceFileDirectly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "example.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := (&ReadTool{}).Execute(testutil.Context(dir), map[string]any{"path": "example.txt"})
	if err != nil || !strings.Contains(out.Text, "1:fixture") {
		t.Fatalf("relative read: %s %v", out.Text, err)
	}
}
