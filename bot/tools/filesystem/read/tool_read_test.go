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

func TestReadToolRegistersEditAwareView(t *testing.T) {
	td := testutil.SetupTemp(t)
	r := &ReadTool{}
	p := filepath.Join(td, "a.go")
	state := tools.NewExecutionState()
	ctx := tools.WithExecutionState(context.Background(), state)

	out, err := r.Execute(ctx, map[string]any{
		"path": p, "startLine": float64(1), "endLine": float64(3),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "\nVIEW rev=") || !strings.Contains(out, " window=W") {
		t.Fatalf("expected VIEW metadata in read output, got:\n%s", out)
	}
	if _, ok := state.ViewStore.Get(extractWindowID(out)); !ok {
		t.Fatalf("expected view store to contain read window; output:\n%s", out)
	}
}

func extractWindowID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "VIEW ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, "window=") {
				return strings.TrimPrefix(field, "window=")
			}
		}
	}
	return ""
}
