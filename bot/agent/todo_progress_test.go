package agent

import (
	"context"
	"fmt"
	"testing"

	"nekocode/bot/extension/tool/builtin/todo"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
)

type todoReminderTool struct{ evidenceCountingTool }

func (*todoReminderTool) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "executed"}, nil
}

func TestTodoReminderFollowsExecutedBatchesAndIsTransient(t *testing.T) {
	a := newTestAgent()
	a.deps.toolRegistry.Register(&todoReminderTool{})
	model := &executionLLM{}
	a.deps.llmClient = model
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "inspect", Status: "in_progress"}})
	for batch := 1; batch <= 12; batch++ {
		// Several invocations in one model response still count as one round.
		calls := []core.ToolCallItem{{ID: fmt.Sprint(batch), Name: "inspect"}, {ID: fmt.Sprintf("%d-b", batch), Name: "inspect"}}
		if a.toolRunner.executeAndFeedback(calls, "", nil) {
			t.Fatal("reminder must not terminate the run")
		}
		if _, err := a.modelRunner.callLLMForTool(); err != nil {
			t.Fatal(err)
		}
		messages := model.messages[len(model.messages)-1]
		if got, want := messagesContain(messages, "[todo_progress]"), batch%6 == 0; got != want {
			t.Fatalf("batch %d: reminder=%t, want %t", batch, got, want)
		}
		snapshot := a.deps.ctxMgr.Snapshot()
		if messagesContain(snapshot.Messages, "[todo_progress]") || messagesContain(snapshot.Transcript, "[todo_progress]") {
			t.Fatal("request reminder entered history")
		}
	}
	if len(a.todoProgressReminder()) != 0 {
		t.Fatal("reminder survived its request")
	}
	if a.deps.ctxMgr.Status().TasksDone {
		t.Fatal("reminder completed work automatically")
	}
}

func TestTodoReminderResetAndUnexecutedCalls(t *testing.T) {
	activity := core.ToolCallResult{Name: "inspect", Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{{ID: "executed"}}}}
	for _, tc := range []struct {
		name   string
		items  []protocol.TodoItem
		result core.ToolCallResult
		want   int
	}{
		{name: "no tasks", result: activity},
		{name: "done", items: []protocol.TodoItem{{Status: "completed"}}, result: activity},
		{name: "updated", items: []protocol.TodoItem{{Status: "pending"}}, result: core.ToolCallResult{Name: "todo_write"}},
		{name: "blocked", items: []protocol.TodoItem{{Status: "pending"}}, result: core.ToolCallResult{Name: "inspect", Error: "blocked"}, want: 4},
		{name: "failed update", items: []protocol.TodoItem{{Status: "pending"}}, result: core.ToolCallResult{Name: "todo_write", Error: "invalid", Evidence: activity.Evidence}, want: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAgent()
			a.run.todoBatches = 4
			a.deps.ctxMgr.SetTodos(tc.items)
			a.observeTodoProgress([]core.ToolCallResult{tc.result})
			if a.run.todoBatches != tc.want || len(a.run.pendingHints) != 0 {
				t.Fatalf("counter=%d hints=%v", a.run.todoBatches, a.run.pendingHints)
			}
			a.Reset()
			if a.run.todoBatches != 0 {
				t.Fatal("counter leaked into next run")
			}
		})
	}
}

func TestTodoReminderNotConsumedByFailedModelRequest(t *testing.T) {
	a := newTestAgent()
	a.run.todoBatches = 6
	a.deps.llmClient = &failingLLM{}
	if _, err := a.modelRunner.callLLMForTool(); err == nil {
		t.Fatal("expected model failure")
	}
	if a.run.todoBatches != 6 {
		t.Fatal("failed request consumed reminder")
	}
}

func TestTodoReminderDoesNotAskForToolsDuringSynthesis(t *testing.T) {
	a := newTestAgent()
	model := &executionLLM{}
	a.deps.llmClient = model
	a.run.todoBatches = 6
	a.modelRunner.synthesize()
	if len(model.messages) != 1 {
		t.Fatal("missing synthesis request")
	}
	if messagesContain(model.messages[0], "[todo_progress]") {
		t.Fatal("tool-free synthesis asks for todo_write")
	}
}

func TestTodoUpdateNotifiesHostAndResetsReminder(t *testing.T) {
	for _, status := range []string{"in_progress", "completed"} {
		t.Run(status, func(t *testing.T) {
			a := newTestAgent()
			var notified bool
			restore := a.BindInteraction(Interaction{Todos: func(items []protocol.TodoItem) {
				notified = len(items) == 1 && items[0].Status == status
				a.deps.ctxMgr.SetTodos(items)
			}})
			defer restore()
			tool := &todo.TodoWriteTool{}
			_, update := a.ToolInteraction()
			tool.SetApplyFn(update)
			a.deps.toolRegistry.Register(tool)
			a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "work", Status: "pending"}})
			a.run.todoBatches = 5
			a.toolRunner.executeAndFeedback([]core.ToolCallItem{{ID: "update", Name: "todo_write", Args: map[string]any{
				"todos": []any{map[string]any{"content": "work", "status": status, "result": "fixture completed"}},
			}}}, "", func(event protocol.StepEvent) {
				if event.Action == protocol.StepActionExecuteTool && (!notified || event.IsError) {
					t.Fatal("todo update was not delivered before tool completion")
				}
			})
			if !notified || a.run.todoBatches != 0 || len(a.todoProgressReminder()) != 0 {
				t.Fatalf("notified=%t counter=%d", notified, a.run.todoBatches)
			}
			if a.deps.ctxMgr.Status().TasksDone != (status == "completed") {
				t.Fatal("context did not receive updated todo state")
			}
		})
	}
}
