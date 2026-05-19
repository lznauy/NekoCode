package builtin

import (
	"context"
	"testing"
)

func TestTodoWriteTool(t *testing.T) {
	tw := &TodoWriteTool{}

	out, err := tw.Execute(context.Background(), map[string]any{
		"todos": `[{"content":"task 1","status":"completed"}]`,
	})
	if err != nil {
		t.Fatalf("todo_write: %v", err)
	}
	if out == "" {
		t.Error("empty output")
	}

	_, err = tw.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing todos")
	}
}
