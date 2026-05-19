package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nekocode/common"
)

// forbiddenTool always returns common.LevelForbidden.
type forbiddenTool struct{ testTool }

func (t *forbiddenTool) DangerLevel(map[string]any) common.DangerLevel { return common.LevelForbidden }

// writeTool returns common.LevelWrite.
type writeTool struct{ testTool }

func (t *writeTool) DangerLevel(map[string]any) common.DangerLevel       { return common.LevelWrite }
func (t *writeTool) ExecutionMode(map[string]any) ExecutionMode   { return ModeSequential }

func TestExecutorBatch(t *testing.T) {
	r := NewRegistry()
	r.Register(&testTool{name: "read"})
	r.Register(&testTool{name: "safe"})
	r.Register(&forbiddenTool{testTool{name: "blocked"}})
	r.Register(&writeTool{testTool{name: "writer"}})
	e := NewExecutor(r)

	// Empty batch.
	results := e.ExecuteBatch(context.Background(), nil)
	if len(results) != 0 {
		t.Error("expected empty results")
	}

	// Forbidden tool is blocked.
	results = e.ExecuteBatch(context.Background(), []ToolCallItem{
		{ID: "1", Name: "blocked"},
	})
	if results[0].Error == "" {
		t.Error("expected forbidden error")
	}

	// Safe tool runs.
	results = e.ExecuteBatch(context.Background(), []ToolCallItem{
		{ID: "2", Name: "safe"},
	})
	if results[0].Error != "" || results[0].Output != "ok" {
		t.Errorf("unexpected result: %+v", results[0])
	}
}

func TestExecutorPlanMode(t *testing.T) {
	r := NewRegistry()
	r.Register(&writeTool{testTool{name: "writer"}})
	e := NewExecutor(r)
	e.SetPlanMode(true)

	results := e.ExecuteBatch(context.Background(), []ToolCallItem{
		{ID: "1", Name: "writer"},
	})
	if results[0].Error == "" {
		t.Error("expected plan mode block")
	}
}

func TestExecutorConfirm(t *testing.T) {
	r := NewRegistry()
	r.Register(&writeTool{testTool{name: "writer"}})
	e := NewExecutor(r)

	// Deny all writes.
	e.SetConfirmFn(func(req common.ConfirmRequest) bool { return false })

	results := e.ExecuteBatch(context.Background(), []ToolCallItem{
		{ID: "1", Name: "writer"},
	})
	if results[0].Error == "" {
		t.Error("expected confirm denial")
	}
}

func TestExecutorReadBeforeWrite(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "existing.go")
	os.WriteFile(p, []byte("package main"), 0644)

	r := NewRegistry()
	r.Register(&testTool{name: "read"})
	r.Register(&writeTool{testTool{name: "write"}})
	e := NewExecutor(r)

	// Write without read → blocked.
	results := e.ExecuteBatch(context.Background(), []ToolCallItem{
		{ID: "1", Name: "write", Args: map[string]any{"path": p}},
	})
	if results[0].Error == "" {
		t.Error("expected read-before-write error")
	}

	// Read then write → allowed.
	results = e.ExecuteBatch(context.Background(), []ToolCallItem{
		{ID: "2", Name: "read", Args: map[string]any{"path": p}},
		{ID: "3", Name: "write", Args: map[string]any{"path": p}},
	})
	if results[1].Error != "" {
		t.Errorf("write after read should succeed: %s", results[1].Error)
	}
}
