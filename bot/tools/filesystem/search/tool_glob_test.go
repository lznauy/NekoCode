package search

import (
	"context"
	"testing"

	"nekocode/bot/tools/filesystem/testutil"
)

func TestGlobTool(t *testing.T) {
	td := testutil.SetupTemp(t)
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
