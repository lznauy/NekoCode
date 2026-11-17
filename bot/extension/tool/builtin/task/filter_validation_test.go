package task

import (
	"context"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"testing"
)

func TestTaskListRejectsInvalidFilterTypes(t *testing.T) {
	tool := NewTaskListTool()
	calls := 0
	tool.Wire(func(context.Context, taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		calls++
		return taskbridge.TaskPage{}, nil
	})
	for _, key := range []string{"query", "status"} {
		for _, value := range []any{nil, []any{"running"}, 42} {
			if _, err := tool.Execute(context.Background(), map[string]any{key: value}); err == nil {
				t.Errorf("invalid %s accepted: %#v", key, value)
			}
		}
	}
	if calls != 0 {
		t.Fatalf("invalid filters reached backend: %d", calls)
	}
}
