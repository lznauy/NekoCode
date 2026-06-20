package tree

import (
	"context"
	"testing"

	"nekocode/bot/tools/filesystem/testutil"
)

func TestTreeTool(t *testing.T) {
	td := testutil.SetupTemp(t)
	tr := &TreeTool{}

	out, err := tr.Execute(context.Background(), map[string]any{"path": td, "depth": float64(2)})
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if out == "" {
		t.Error("expected tree output")
	}

	_, err = tr.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing path")
	}
}
