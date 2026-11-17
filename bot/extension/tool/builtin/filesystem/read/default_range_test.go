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
