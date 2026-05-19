package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTool(t *testing.T) {
	td := setupTemp(t)
	w := &WriteTool{}
	p := filepath.Join(td, "new.txt")

	out, err := w.Execute(context.Background(), map[string]any{"path": p, "content": "hello"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if out == "" {
		t.Error("empty output")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", string(data), "hello")
	}
}
