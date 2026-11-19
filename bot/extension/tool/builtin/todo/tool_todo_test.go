package todo

import (
	"context"
	"nekocode/protocol"
	"strings"
	"testing"
)

func TestTodoWriteTool(t *testing.T) {
	tw := &TodoWriteTool{}
	tw.SetApplyFn(func(update protocol.TodoUpdate) (protocol.TodoPlan, error) {
		items, err := protocol.ReconcileTodos(nil, update.Items)
		return protocol.TodoPlan{ID: "fixture", Items: items}, err
	})
	params := tw.Parameters()
	if len(params) != 3 || params[0].Type != "array" || params[0].Items == nil || params[0].Items.Properties["status"].Enum[1] != "in_progress" {
		t.Fatalf("todo schema does not describe structured items: %+v", params)
	}

	out, err := tw.Execute(context.Background(), map[string]any{
		"todos": []any{map[string]any{"content": "task 1", "status": "completed", "result": strings.Repeat("测", 24)}},
	})
	if err != nil {
		t.Fatalf("todo_write: %v", err)
	}
	if !strings.Contains(out.Render(), "[result: "+strings.Repeat("测", 21)+"…]") || !strings.Contains(out.Render(), "marked completed") {
		t.Fatalf("missing saved result or status: %s", out.Render())
	}

	_, err = tw.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing todos")
	}
}
