package builtin

import (
	"nekocode/common"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"nekocode/bot/tools"
)

type TodoWriteTool struct {
	mu       sync.Mutex
	onUpdate common.TodoFunc
	items    []common.TodoItem
}

func NewTodoWriteTool() *TodoWriteTool {
	return &TodoWriteTool{}
}

func (t *TodoWriteTool) SetUpdateFn(fn common.TodoFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onUpdate = fn
}

func (t *TodoWriteTool) Name() string                                            { return "todo_write" }
func (t *TodoWriteTool) ExecutionMode(map[string]interface{}) tools.ExecutionMode { return tools.ModeSequential }
func (t *TodoWriteTool) DangerLevel(map[string]interface{}) common.DangerLevel     { return common.LevelSafe }
func (t *TodoWriteTool) Description() string {
	return "Update the task list (record only, not for planning). Each call fully replaces the list. Write the complete list in one call — never append. Format: [{\"content\":\"...\",\"status\":\"pending|in_progress|completed\"}]"
}

func (t *TodoWriteTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "todos", Type: "string", Required: true, Description: "JSON task list: [{\"content\":\"...\",\"status\":\"pending|in_progress|completed\"}]"},
	}
}

func (t *TodoWriteTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	var items []common.TodoItem
	switch v := args["todos"].(type) {
	case string:
		if v == "" {
			return "", fmt.Errorf("missing todos parameter")
		}
		if err := json.Unmarshal([]byte(v), &items); err != nil {
			return "", fmt.Errorf("failed to parse todos: %v", err)
		}
	case []interface{}:
		raw, _ := json.Marshal(v)
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", fmt.Errorf("failed to parse todos: %v", err)
		}
	default:
		return "", fmt.Errorf("invalid todos format: expected JSON string or array, got %T", args["todos"])
	}

	t.mu.Lock()
	t.items = items
	fn := t.onUpdate
	t.mu.Unlock()

	if fn != nil {
		fn(items)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Task list updated (%d items):\n", len(items))
	done := 0
	for _, it := range items {
		if it.Status == "completed" {
			done++
		}
	}
	for i, it := range items {
		icon := "·" // · pending
		switch it.Status {
		case "in_progress":
			icon = "▸" // ▸ in progress
		case "completed":
			icon = "✓" // ✓ completed
		}
		fmt.Fprintf(&b, "%d. %s %s\n", i+1, icon, it.Content)
	}
	if done == len(items) {
		fmt.Fprintf(&b, "All %d tasks complete.", done)
	}
	return b.String(), nil
}
