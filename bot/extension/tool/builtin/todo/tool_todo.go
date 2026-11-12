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
	mu    sync.Mutex
	apply protocol.TodoUpdateFunc
}

func (t *TodoWriteTool) SetApplyFn(fn protocol.TodoUpdateFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.apply = fn
}

func (t *TodoWriteTool) Name() string { return "todo_write" }
func (t *TodoWriteTool) Description() string {
	return "Maintain a delivery checklist for multi-step work in any mode. Replace todos in the active plan; reuse returned item IDs when renaming or updating. At most one item may be in_progress. Completed items require a brief result; blocked/cancelled items require a reason. Never omit unfinished items: retain or explicitly cancel them with a reason. Tool or subagent links are optional. To resume a saved plan in this session, supply plan_id; omit todos to activate and retrieve that plan before continuing. This activates unfinished work, so use it only when continuing the relevant demand. Reconcile the checklist before the final answer. Tool success alone does not prove completion."
}

func (t *TodoWriteTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{
			Name: "todos", Type: "array", Description: "Complete replacement task list.",
			Items: &core.Schema{
				Type: "object", Required: []string{"content", "status"},
				Properties: map[string]core.Schema{
					"content": {Type: "string", Description: "Concrete deliverable or verification step."},
					"status":  {Type: "string", Enum: []string{"pending", "in_progress", "completed", "blocked", "cancelled"}},
					"id":      {Type: "string", Description: "Saved step ID. Omit only for a new step or an unchanged legacy description."},
					"result":  {Type: "string", Description: "Brief actual outcome, optionally including file/test/task references; required when completing a step."},
					"reason":  {Type: "string", Description: "Concrete reason for blocked/cancelled steps or reopening a settled step."},
				},
			},
		},
		{Name: "plan_id", Type: "string", Description: "Optional saved plan ID to activate and continue in this session; omitted means the active demand."},
	}
}

func (t *TodoWriteTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolOutput{}, err
	}
	var planID string
	if raw, ok := args["plan_id"]; ok {
		var valid bool
		planID, valid = raw.(string)
		if !valid || strings.TrimSpace(planID) == "" {
			return core.ToolOutput{}, fmt.Errorf("plan_id must be a nonempty string")
		}
	}
	_, hasTodos := args["todos"]
	if !hasTodos && planID == "" {
		return core.ToolOutput{}, fmt.Errorf("todos or a saved plan_id is required")
	}
	var items []protocol.TodoItem
	if hasTodos {
		switch v := args["todos"].(type) {
		case string:
			if v == "" {
				return core.ToolOutput{}, fmt.Errorf("missing todos parameter")
			}
			if err := json.Unmarshal([]byte(v), &items); err != nil {
				return core.ToolOutput{}, fmt.Errorf("failed to parse todos: %w", err)
			}
		case []any:
			raw, err := json.Marshal(v)
			if err != nil {
				return core.ToolOutput{}, fmt.Errorf("invalid todos: %w", err)
			}
			if err := json.Unmarshal(raw, &items); err != nil {
				return core.ToolOutput{}, fmt.Errorf("failed to parse todos: %w", err)
			}
		default:
			return core.ToolOutput{}, fmt.Errorf("invalid todos format: expected JSON string or array, got %T", args["todos"])
		}

	}
	t.mu.Lock()
	fn := t.apply
	t.mu.Unlock()

	if fn == nil {
		return core.ToolOutput{}, fmt.Errorf("todo plan storage is not available")
	}
	plan, err := fn(protocol.TodoUpdate{PlanID: planID, Items: items, SelectOnly: !hasTodos})
	if err != nil {
		return core.ToolOutput{}, err
	}
	items = plan.Items
	var b strings.Builder
	fmt.Fprintf(&b, "Plan %s (%d items):\n", plan.ID, len(items))
	done := protocol.CountCompleted(items)
	for i, it := range items {
		fmt.Fprintf(&b, "%d. [%s] %s %s %s", i+1, it.ID, todoStatusIcon(it.Status), it.Status, protocol.SubagentTextPreview(it.Content, 64))
		if it.Reason != "" {
			fmt.Fprintf(&b, " (%s)", protocol.SubagentTextPreview(it.Reason, 64))
		}
		b.WriteByte('\n')
	}
	if len(items) > 0 && done == len(items) {
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
	case "blocked":
		return "!"
	case "cancelled":
		return "–"
	default:
		return "·"
	}
}
