package edit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditRequiresExplicitReplacement(t *testing.T) {
	for _, replacement := range []map[string]any{{}, {"newString": nil}, {"newString": 42}, {"new_string": "new"}, {"newString": "new", "replaceAll": "true"}} {
		t.Run("invalid", func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file.txt")
			writeFile(t, path, "keep target\n")
			args := map[string]any{"path": path, "oldString": "target"}
			for k, v := range replacement {
				args[k] = v
			}
			if _, err := (&EditTool{}).Execute(editTestContext(dir), args); err == nil {
				t.Fatal("invalid edit succeeded")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "keep target\n" {
				t.Fatalf("invalid edit changed file: %q %v", data, err)
			}
		})
	}
}

func TestEditAllowsExplicitDeletion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeFile(t, path, "keep target\n")
	if _, err := (&EditTool{}).Execute(editTestContext(dir), map[string]any{"path": path, "oldString": "target", "newString": ""}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "keep \n" {
		t.Fatal(got)
	}
	for _, p := range (&EditTool{}).Parameters() {
		if (p.Name == "oldString" || p.Name == "newString") && !p.Required {
			t.Fatalf("%s must be required in schema", p.Name)
		}
	}
}
