package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/taskbridge"
)

func TestTaskListReturnsBoundedCurrentSessionPage(t *testing.T) {
	tool := NewTaskListTool()
	var got taskbridge.TaskListFilter
	tool.Wire(func(_ context.Context, filter taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		got = filter
		return taskbridge.TaskPage{SessionID: "parent", Total: 1, NextOffset: 1, Items: []taskbridge.TaskSummary{{
			ID: "12345678-1234-1234-1234-123456789abc", Profile: "explore", Status: "completed", Prompt: "inspect", RunCount: 1,
			LatestAttempt: &taskbridge.TaskAttemptSummary{ID: "attempt-1", Seq: 1, Status: "completed", Result: "done"},
		}}}, nil
	})
	out, err := tool.Execute(context.Background(), map[string]any{"query": "inspect", "status": "completed", "offset": float64(0), "limit": float64(10)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Query != "inspect" || got.Status != "completed" || got.Limit != 10 {
		t.Fatalf("filter = %+v", got)
	}
	if !strings.Contains(out.Text, "12345678-1234-1234-1234-123456789abc") || !strings.Contains(out.Text, "latest_attempt") || !strings.Contains(out.Text, "attempt-1") {
		t.Fatalf("output lost task or attempt identity: %s", out.Text)
	}
}

func TestTaskListBoundsFieldsAndTotalOutput(t *testing.T) {
	tool := NewTaskListTool()
	tool.Wire(func(context.Context, taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		items := make([]taskbridge.TaskSummary, 100)
		for i := range items {
			items[i] = taskbridge.TaskSummary{
				ID: fmt.Sprintf("task-%03d", i), Profile: "coder", Status: "completed",
				Title: strings.Repeat("标题", 200), Prompt: strings.Repeat("需求", 2000), Result: strings.Repeat("结果", 8000),
				LatestAttempt: &taskbridge.TaskAttemptSummary{ID: fmt.Sprintf("attempt-%03d", i), Prompt: strings.Repeat("输入", 2000), Result: strings.Repeat("输出", 8000)},
			}
		}
		return taskbridge.TaskPage{SessionID: "parent", Total: 100, NextOffset: 100, Items: items}, nil
	})
	out, err := tool.Execute(context.Background(), map[string]any{"limit": float64(100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Text) > taskListOutputBudget {
		t.Fatalf("output budget = %d, got %d", taskListOutputBudget, len(out.Text))
	}
	var page taskbridge.TaskPage
	if err := json.Unmarshal([]byte(out.Text), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 || len(page.Items) == 100 || !page.HasMore || page.NextOffset != len(page.Items) {
		t.Fatalf("bounded pagination = count %d next %d more %v", len(page.Items), page.NextOffset, page.HasMore)
	}
	if !strings.Contains(out.Text, "truncated") || strings.Contains(out.Text, strings.Repeat("结果", 8000)) {
		t.Fatal("long fields were not truncated")
	}
}

func TestTaskListValidatesPaginationAndWiring(t *testing.T) {
	tool := NewTaskListTool()
	if _, err := tool.Execute(context.Background(), nil); err == nil {
		t.Fatal("unwired task_list succeeded")
	}
	called := false
	tool.Wire(func(context.Context, taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		called = true
		return taskbridge.TaskPage{}, nil
	})
	for _, args := range []map[string]any{{"offset": float64(-1)}, {"limit": float64(0)}, {"limit": float64(101)}, {"limit": 1.5}} {
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Fatalf("invalid pagination accepted: %+v", args)
		}
	}
	if called {
		t.Fatal("invalid input reached lister")
	}
	if _, err := tool.Execute(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("default query did not reach lister")
	}
}
