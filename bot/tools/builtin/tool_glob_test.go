package builtin

import (
	"context"
	"testing"
)

func TestGlobTool(t *testing.T) {
	td := setupTemp(t)
	g := &GlobTool{}

	out, err := g.Execute(context.Background(), map[string]any{"pattern": "*.go", "path": td})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if out == "" {
		t.Error("expected files matching *.go")
	}

	_, err = g.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}
