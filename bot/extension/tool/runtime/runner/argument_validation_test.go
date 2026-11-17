package runner_test

import (
	"context"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/filesystem/edit"
	"nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/runner"
	"os"
	"path/filepath"
	"testing"
)

func TestExecutorRejectsMissingEditReplacementDespiteReadEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	before := []byte("keep target\n")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	registry := tools.New(&edit.EditTool{})
	registry.Workspace().Configure(dir, nil)
	executor := runner.NewExecutor(registry)
	id := executor.ExecutionState().Reads.Record(path, before, "delivered read")
	executor.ExecutionState().Reads.Observe(map[string]string{id: "delivered read"})
	result := executor.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "invalid-edit", Name: "edit", Args: map[string]any{"path": path, "oldString": "target"}}})[0]
	if result.Error == "" {
		t.Fatal("missing replacement accepted by full executor")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(before) {
		t.Fatalf("invalid edit changed target: %q %v", data, err)
	}
}

func TestExecutorEmptyWriteStillRequiresDeliveredRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	before := []byte("keep\n")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	registry := tools.New(&write.WriteTool{})
	registry.Workspace().Configure(dir, nil)
	executor := runner.NewExecutor(registry)
	call := core.ToolCallItem{ID: "empty-write", Name: "write", Args: map[string]any{"path": path, "content": ""}}
	if result := executor.ExecuteBatch(context.Background(), []core.ToolCallItem{call})[0]; result.Error == "" {
		t.Fatal("empty write bypassed read requirement")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(before) {
		t.Fatalf("write without read changed file: %q %v", data, err)
	}
	id := executor.ExecutionState().Reads.Record(path, before, "delivered read")
	executor.ExecutionState().Reads.Observe(map[string]string{id: "delivered read"})
	if result := executor.ExecuteBatch(context.Background(), []core.ToolCallItem{call})[0]; result.Error != "" {
		t.Fatal(result.Error)
	}
	data, err = os.ReadFile(path)
	if err != nil || len(data) != 0 {
		t.Fatalf("explicit empty write failed: %q %v", data, err)
	}
}
