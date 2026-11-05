package read

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

func TestReadTool(t *testing.T) {
	td := testutil.SetupTemp(t)
	r := &ReadTool{}
	p := filepath.Join(td, "a.go")

	_, err := r.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing path")
	}

	out, err := r.Execute(testutil.Context(td), map[string]any{
		"path": p, "startLine": float64(1), "endLine": float64(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Render() == "" {
		t.Error("empty output")
	}
}

func TestReadUTF8AcrossBinarySampleBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "utf8.txt")
	text := strings.Repeat("a", 65535) + "中\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := (&ReadTool{}).Execute(testutil.Context(root), map[string]any{"path": path, "startLine": float64(1), "endLine": float64(1)})
	if err != nil || !strings.Contains(out.Render(), "中") {
		t.Fatalf("valid UTF-8 rejected: %v", err)
	}
	for _, data := range [][]byte{[]byte("abc\x00"), []byte("abc\xff"), []byte("abc\xe4\xb8")} {
		if !isBinary(data) {
			t.Errorf("invalid text accepted: %x", data)
		}
	}
}

func TestReadToolRecordsSnapshotInExecutionState(t *testing.T) {
	td := testutil.SetupTemp(t)
	r := &ReadTool{}
	p := filepath.Join(td, "a.go")
	state := execution.NewExecutionState()
	ctx := execution.WithExecutionState(testutil.Context(td), state)

	_, err := r.Execute(ctx, map[string]any{
		"path": p, "startLine": float64(1), "endLine": float64(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tag := toolutil.ComputeFileHash("package main\n\nfunc main() {}\n")
	if snap := state.SnapshotStore.ByHash(p, tag); snap == nil {
		t.Fatalf("expected snapshot %s in execution state", tag)
	}
}

func TestReadToolDoesNotEmitEditViewMetadata(t *testing.T) {
	td := testutil.SetupTemp(t)
	r := &ReadTool{}
	p := filepath.Join(td, "a.go")

	out, err := r.Execute(testutil.Context(td), map[string]any{
		"path": p, "startLine": float64(1), "endLine": float64(3),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out.Render(), "\nVIEW rev=") || strings.Contains(out.Render(), " window=W") {
		t.Fatalf("did not expect VIEW metadata in read output, got:\n%s", out.Render())
	}
	if !strings.Contains(out.Render(), "[") || !strings.Contains(out.Render(), "#") || !strings.Contains(out.Render(), "1:package main") {
		t.Fatalf("expected tag header and line output, got:\n%s", out.Render())
	}
}

func TestReadRejectsOversizedTextBeforeAllocating(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxReadFileBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if _, err := (&ReadTool{}).Execute(testutil.Context(dir), map[string]any{"path": path, "startLine": float64(1), "endLine": float64(1)}); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized text accepted: %v", err)
	}
}
