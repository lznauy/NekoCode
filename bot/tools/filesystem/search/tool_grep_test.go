package search

import (
	"context"
	"testing"

	"nekocode/bot/tools/filesystem/testutil"
)

func TestGrepTool(t *testing.T) {
	td := testutil.SetupTemp(t)
	g := &GrepTool{}

	out, err := g.Execute(context.Background(), map[string]any{"pattern": "func", "path": td})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if out == "" {
		t.Error("expected matches for 'func'")
	}

	_, err = g.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}
