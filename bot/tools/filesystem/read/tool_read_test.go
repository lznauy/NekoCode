package read

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/tools"
	"nekocode/bot/tools/editcore"
	"nekocode/bot/tools/filesystem/testutil"
)

func TestReadTool(t *testing.T) {
	td := testutil.SetupTemp(t)
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

func TestReadToolRecordsSnapshotInExecutionState(t *testing.T) {
	td := testutil.SetupTemp(t)
	r := &ReadTool{}
	p := filepath.Join(td, "a.go")
	state := tools.NewExecutionState()
	ctx := tools.WithExecutionState(context.Background(), state)

	_, err := r.Execute(ctx, map[string]any{
		"path": p, "startLine": float64(1), "endLine": float64(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tag := editcore.ComputeFileHash("package main\n\nfunc main() {}\n")
	if snap := state.SnapshotStore.ByHash(p, tag); snap == nil {
		t.Fatalf("expected snapshot %s in execution state", tag)
	}
}

func TestReadToolDoesNotEmitEditViewMetadata(t *testing.T) {
	td := testutil.SetupTemp(t)
	r := &ReadTool{}
	p := filepath.Join(td, "a.go")

	out, err := r.Execute(context.Background(), map[string]any{
		"path": p, "startLine": float64(1), "endLine": float64(3),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "\nVIEW rev=") || strings.Contains(out, " window=W") {
		t.Fatalf("did not expect VIEW metadata in read output, got:\n%s", out)
	}
	if !strings.Contains(out, "[") || !strings.Contains(out, "#") || !strings.Contains(out, "1:package main") {
		t.Fatalf("expected tag header and line output, got:\n%s", out)
	}
}
