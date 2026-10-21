package todo

import (
	"context"
	"testing"
)

func TestTodoWriteTool(t *testing.T) {
	tw := &TodoWriteTool{}
	params := tw.Parameters()
	if len(params) != 1 || params[0].Type != "array" || params[0].Items == nil || params[0].Items.Properties["status"].Enum[1] != "in_progress" {
		t.Fatalf("todo schema does not describe structured items: %+v", params)
	}

	out, err := tw.Execute(context.Background(), map[string]any{
		"todos": []any{map[string]any{"content": "task 1", "status": "completed"}},
	})
	if err != nil {
		t.Fatalf("todo_write: %v", err)
	}
	if out.Render() == "" {
		t.Error("empty output")
	}

	_, err = tw.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing todos")
	}
}
