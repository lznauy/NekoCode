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
	return "Maintain a delivery checklist. New plan example: omit plan_id and every item id; the returned plan supplies IDs. To add a step later, use patch=true and omit that new step's id; to update an existing step, use its exact returned id. Never invent plan or step IDs. Patch updates preserve other work; default patch=false replaces the full list and must retain unfinished steps or cancel them with a reason. Multiple independent steps may be in_progress when they are actually running in parallel; leave queued work pending. Completed steps need a brief result. Supply an exact saved plan_id without todos only to activate that same demand. Reconcile before final answer; tool success does not prove completion."
}

func (t *TodoWriteTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{
			Name: "todos", Type: "array", Description: "Changed steps when patch=true; otherwise the complete replacement list.",
			Items: &core.Schema{
				Type: "object",
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
		{Name: "patch", Type: "boolean", Description: "Default false for compatibility. Prefer true to update only listed steps without resending the whole plan. Existing steps need returned id; new steps omit id and require content/status."},
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
	patch := false
	if raw, exists := args["patch"]; exists {
		var valid bool
		patch, valid = raw.(bool)
		if !valid || !hasTodos {
			return core.ToolOutput{}, fmt.Errorf("patch must be boolean and requires todos")
		}
	}
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
	plan, err := fn(protocol.TodoUpdate{PlanID: planID, Items: items, SelectOnly: !hasTodos, Patch: patch})
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
