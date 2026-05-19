package builtin

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReadTool(t *testing.T) {
	td := setupTemp(t)
	r := &ReadTool{}
	p := filepath.Join(td, "a.go")

	_, err := r.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing path")
	}

	out, err := r.Execute(context.Background(), map[string]any{
		"path": p, "startLine": float64(1), "endLine": float64(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("empty output")
	}
}
