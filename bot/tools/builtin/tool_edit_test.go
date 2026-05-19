package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEditTool(t *testing.T) {
	td := setupTemp(t)
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	out, err := e.Execute(context.Background(), map[string]any{
		"path": p, "old_string": "line2\n", "new_string": "replaced\n",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if out == "" {
		t.Error("empty output")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "line1\nreplaced\nline3\n" {
		t.Errorf("unexpected content: %q", string(data))
	}

	// old_string not found
	_, err = e.Execute(context.Background(), map[string]any{
		"path": p, "old_string": "NOTFOUND", "new_string": "x",
	})
	if err == nil {
		t.Error("expected error for non-matching old_string")
	}
}
