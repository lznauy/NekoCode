package tools

import (
	"context"
	"testing"

	"nekocode/common"
)

// forbiddenTool always returns common.LevelForbidden.
type forbiddenTool struct{ testTool }

func (t *forbiddenTool) DangerLevel(map[string]any) common.DangerLevel { return common.LevelForbidden }

// writeTool returns common.LevelWrite.
type writeTool struct{ testTool }

func (t *writeTool) DangerLevel(map[string]any) common.DangerLevel { return common.LevelWrite }
func (t *writeTool) ExecutionMode(map[string]any) ExecutionMode    { return ModeSequential }

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

func TestExecutorBatchPreservesCallOrderAcrossModes(t *testing.T) {
	r := NewRegistry()
	r.Register(&testTool{name: "read"})
	r.Register(&writeTool{testTool{name: "write"}})
	e := NewExecutor(r)

	results := e.ExecuteBatch(context.Background(), []ToolCallItem{
		{ID: "1", Name: "write", Args: map[string]any{"path": "a.go"}},
		{ID: "2", Name: "read", Args: map[string]any{"path": "a.go"}},
		{ID: "3", Name: "write", Args: map[string]any{"path": "b.go"}},
	})

	for i, wantID := range []string{"1", "2", "3"} {
		if results[i].ID != wantID {
			t.Fatalf("result %d has ID %q, want %q; results=%+v", i, results[i].ID, wantID, results)
		}
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

func TestExecutorDoesNotOwnReadBeforeWriteGovernance(t *testing.T) {
	r := NewRegistry()
	r.Register(&testTool{name: "read"})
	r.Register(&writeTool{testTool{name: "write"}})
	e := NewExecutor(r)

	// Read-before-write is governed by agent ledger policy, not executor-local
	// state. The executor must not reject a call solely because it lacks local
	// read history, otherwise bash/read evidence recorded in the ledger is ignored.
	results := e.ExecuteBatch(context.Background(), []ToolCallItem{
		{ID: "1", Name: "write", Args: map[string]any{"path": "existing.go"}},
	})
	if results[0].Error != "" {
		t.Errorf("executor should not apply read-before-write governance: %s", results[0].Error)
	}
}
