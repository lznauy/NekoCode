package runner

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"nekocode/bot/tools/core"
	"nekocode/common"
)

type fakeRegistry map[string]core.Tool

func (r fakeRegistry) Get(name string) (core.Tool, error) {
	if t, ok := r[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("tool not found: %s", name)
}

type fakeTool struct {
	name   string
	mode   core.ExecutionMode
	danger common.DangerLevel
	output string
}

func (t fakeTool) Name() string                                    { return t.name }
func (t fakeTool) Description() string                             { return "test" }
func (t fakeTool) Parameters() []core.Parameter                    { return nil }
func (t fakeTool) ExecutionMode(map[string]any) core.ExecutionMode { return t.mode }
func (t fakeTool) DangerLevel(map[string]any) common.DangerLevel   { return t.danger }
func (t fakeTool) Execute(context.Context, map[string]any) (string, error) {
	if t.output != "" {
		return t.output, nil
	}
	return "ok", nil
}

func TestExecutorBatchPreservesCallOrderAcrossModes(t *testing.T) {
	e := NewExecutor(fakeRegistry{
		"read":  fakeTool{name: "read", mode: core.ModeParallel, danger: common.LevelSafe},
		"write": fakeTool{name: "write", mode: core.ModeSequential, danger: common.LevelWrite},
	})

	results := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
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

func TestExecutorBlocksForbiddenAndPlanMode(t *testing.T) {
	e := NewExecutor(fakeRegistry{
		"blocked": fakeTool{name: "blocked", mode: core.ModeParallel, danger: common.LevelForbidden},
		"writer":  fakeTool{name: "writer", mode: core.ModeSequential, danger: common.LevelWrite},
	})

	if got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "blocked"}})[0]; got.Error == "" {
		t.Fatal("expected forbidden error")
	}

	e.SetPlanMode(true)
	if got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "2", Name: "writer"}})[0]; got.Error == "" {
		t.Fatal("expected plan mode error")
	}
}

func TestExecutorConfirmDenial(t *testing.T) {
	e := NewExecutor(fakeRegistry{
		"writer": fakeTool{name: "writer", mode: core.ModeSequential, danger: common.LevelWrite},
	})
	e.SetConfirmFn(func(common.ConfirmRequest) bool { return false })

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "writer"}})[0]
	if got.Error == "" {
		t.Fatal("expected confirm denial")
	}
}

func TestTruncateOutput(t *testing.T) {
	var output string
	for i := range maxLines + 5 {
		output += fmt.Sprintf("line %d\n", i)
	}
	got := truncateOutput(output)
	if len(got) >= len(output) {
		t.Fatal("expected truncated output")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("missing truncation marker: %q", got)
	}
}
