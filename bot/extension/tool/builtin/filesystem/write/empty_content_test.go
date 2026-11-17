package write

import (
	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAllowsExplicitEmptyContent(t *testing.T) {
	for _, exists := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.txt")
		if exists {
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := (&WriteTool{}).Execute(testutil.Context(dir), map[string]any{"path": path, "content": ""}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) != 0 {
			t.Fatalf("empty write failed: %q %v", data, err)
		}
	}
}

func TestWriteRejectsMissingOrInvalidContent(t *testing.T) {
	for _, args := range []map[string]any{{}, {"content": nil}, {"content": 123}} {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("keep"), 0600)
		args["path"] = path
		if _, err := (&WriteTool{}).Execute(testutil.Context(dir), args); err == nil {
			t.Fatal("invalid content accepted")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "keep" {
			t.Fatalf("invalid write mutated target: %q %v", data, err)
		}
	}
}
