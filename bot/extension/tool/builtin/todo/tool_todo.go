package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/protocol"
)

type TodoWriteTool struct {
	toolutil.SequentialSafeTool
	mu       sync.Mutex
	onUpdate protocol.TodoFunc
}

func (t *TodoWriteTool) SetUpdateFn(fn protocol.TodoFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onUpdate = fn
}

func (t *TodoWriteTool) Name() string { return "todo_write" }
func (t *TodoWriteTool) Description() string {
	return "Replace the complete task list, tracking requested deliverables and necessary verification; skip for simple work. Keep at most one in_progress item. Mark a step in_progress before starting; promptly complete it and activate the next step when finished, rather than batching updates at the end. Revise the list when blocked or plans change, and reconcile it before the final answer. Update at deliverable boundaries, not every tool call; tool success alone does not prove completion. Never mark unfinished work completed."
}

func (t *TodoWriteTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{
			Name: "todos", Type: "array", Required: true, Description: "Complete replacement task list.",
			Items: &core.Schema{
				Type: "object", Required: []string{"content", "status"},
				Properties: map[string]core.Schema{
					"content": {Type: "string", Description: "Concrete deliverable or verification step."},
					"status":  {Type: "string", Enum: []string{"pending", "in_progress", "completed"}},
				},
			},
		},
	}
}

func (t *TodoWriteTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	var items []protocol.TodoItem
	switch v := args["todos"].(type) {
	case string:
		if v == "" {
			return core.ToolOutput{}, fmt.Errorf("missing todos parameter")
		}
		if err := json.Unmarshal([]byte(v), &items); err != nil {
			return core.ToolOutput{}, fmt.Errorf("failed to parse todos: %w", err)
		}
	case []any:
		raw, _ := json.Marshal(v)
		if err := json.Unmarshal(raw, &items); err != nil {
			return core.ToolOutput{}, fmt.Errorf("failed to parse todos: %w", err)
		}
	default:
		return core.ToolOutput{}, fmt.Errorf("invalid todos format: expected JSON string or array, got %T", args["todos"])
	}

	t.mu.Lock()
	fn := t.onUpdate
	t.mu.Unlock()

	if fn != nil {
		fn(items)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Task list updated (%d items):\n", len(items))
	done := protocol.CountCompleted(items)
	for i, it := range items {
		fmt.Fprintf(&b, "%d. %s %s\n", i+1, todoStatusIcon(it.Status), it.Content)
	}
	if done == len(items) {
		fmt.Fprintf(&b, "All %d tasks complete.", done)
	}
	return core.ToolOutput{Text: b.String()}, nil
}

func todoStatusIcon(status string) string {
	switch status {
	case "in_progress":
		return "▸"
	case "completed":
		return "✓"
	default:
		return "·"
	}
}
