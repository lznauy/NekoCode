package agent

import (
	"context"
	"fmt"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/todo"
	"nekocode/bot/provider/types"
	"strings"
	"testing"

	"nekocode/mode"
	"nekocode/protocol"
)

func coordinatorContext(ctx context.Context, pendingWorkers int) context.Context {
	definition, _ := mode.Resolve(mode.SuperAgent)
	return protocol.WithCoordination(mode.WithSnapshot(ctx, definition.Snapshot), protocol.Coordination{
		GroupID: "demand", Notification: true, PendingTasks: pendingWorkers,
	})
}

func coordinatorWithTodos(t *testing.T, pendingWorkers int) *Agent {
	t.Helper()
	a := newTestAgent()
	a.life.SetRunContext(coordinatorContext(t.Context(), pendingWorkers))
	t.Cleanup(func() { a.life.SetRunContext(nil) })
	a.deps.ctxMgr.SelectTodoGroup("demand")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{
		{Content: "inspect", Status: "completed"},
		{Content: "fix", Status: "pending"},
		{Content: "verify", Status: "pending"},
	})
	return a
}

func TestCoordinatorCannotFinishAfterOnlyFirstPlanStep(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	finished := a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "Inspection complete, here is the summary."}, nil)
	if finished || a.run.finalText != "" || len(a.run.pendingHints) != 1 || !strings.Contains(a.run.pendingHints[0].Content, "verify") {
		t.Fatalf("unfinished plan accepted: finished=%t final=%q hints=%+v", finished, a.run.finalText, a.run.pendingHints)
	}
	for _, msg := range a.deps.ctxMgr.Snapshot().Messages {
		if msg.Content == "Inspection complete, here is the summary." {
			t.Fatal("premature answer was committed")
		}
	}
}
func TestCoordinatorMayYieldForWorkersOrFinishCompletedPlan(t *testing.T) {
	for _, tc := range []string{"running", "dispatched", "complete", "standard", "unrelated"} {
		t.Run(tc, func(t *testing.T) {
			a := coordinatorWithTodos(t, 0)
			switch tc {
			case "running":
				a = coordinatorWithTodos(t, 1)
			case "dispatched":
				a.run.todoDispatched = true
				a.run.todoDispatchPlan = a.deps.ctxMgr.ActiveTodoPlanID()
			case "complete":
				a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "done", Status: "completed"}})
			case "standard":
				a.life.SetRunContext(context.Background())
			case "unrelated":
				definition, _ := mode.Resolve(mode.SuperAgent)
				a.life.SetRunContext(mode.WithSnapshot(context.Background(), definition.Snapshot))
			}
			if !a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "Progress or answer."}, nil) || a.run.err != nil {
				t.Fatal("legitimate yield/answer blocked")
			}
		})
	}
}
func TestCoordinatorUnfinishedPlanCorrectionIsBounded(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	for i := 0; i < 3; i++ {
		if a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "done"}, nil) {
			t.Fatal("correction stopped early")
		}
	}
	if !a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "Partial review report."}, nil) || a.run.err != nil || !a.run.partialCloseout {
		t.Fatal("stuck coordinator did not produce a partial closeout")
	}
	if !strings.Contains(a.run.finalText, "Partial review report.") || !strings.Contains(a.run.finalText, "verify") || !strings.Contains(a.run.finalText, "未完成") {
		t.Fatalf("partial report or unfinished items missing: %q", a.run.finalText)
	}
}
func TestCoordinatorPlanProgressAllowsFurtherCorrection(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	a.run.todoCompletionChecks = 3
	a.run.todoCheckedPlan = a.deps.ctxMgr.ActiveTodoPlanID()
	a.run.todoCompleted = 0
	if a.turnRunner.continueUnfinishedPlan() != true || a.run.err != nil || a.run.todoCompletionChecks != 1 {
		t.Fatal("real plan progress did not reset correction budget")
	}
}

func TestCoordinatorLoopContinuesAfterPrematureSummary(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	model := &evidenceSequenceLLM{scripts: [][]types.StreamToken{
		{{Content: "Only inspection is done."}},
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "update", Name: "todo_write", Arguments: `{"todos":[{"content":"inspect","status":"completed"},{"content":"fix","status":"completed","result":"fix verified"},{"content":"verify","status":"completed","result":"tests passed"}]}`}}},
		{{Content: "All planned work and validation are complete."}},
	}}
	a.deps.llmClient = model
	todoTool := &todo.TodoWriteTool{}
	todoTool.SetApplyFn(a.updateTodos)
	a.deps.toolRegistry.Register(todoTool)
	result := a.RunContext(coordinatorContext(t.Context(), 0), "Review the completed worker evidence and continue the plan.", nil, nil)
	if result.Error != nil || model.calls != 3 || result.FinalOutput != "All planned work and validation are complete." || !a.deps.ctxMgr.Status().TasksDone {
		t.Fatalf("coordinator stopped before reconciling plan: calls=%d result=%+v", model.calls, result)
	}
}

func TestCoordinatorNotificationSelectsOwnPlanAtRealRunEntry(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	a.deps.ctxMgr.SelectTodoGroup("other-demand")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "unrelated", Status: "completed"}})
	model := &evidenceSequenceLLM{scripts: [][]types.StreamToken{
		{{Content: "Only inspection is done."}},
		{{Content: "Still only inspection is done."}},
		{{Content: "Inspection summary."}},
		{{Content: "Stopping prematurely."}},
	}}
	a.deps.llmClient = model
	result := a.RunContext(coordinatorContext(t.Context(), 0), "Review demand completion.", nil, nil)
	if result.Error != nil || result.StopReason != "incomplete" || !strings.Contains(result.FinalOutput, "verify") || model.calls != 4 || strings.Contains(result.FinalOutput, "unrelated") {
		t.Fatalf("notification used another demand plan: calls=%d result=%+v", model.calls, result)
	}
}

func TestChecklistContinuesInEveryModeWithoutSubagents(t *testing.T) {
	for _, id := range []mode.ID{mode.Standard, mode.Plan, mode.SuperAgent} {
		t.Run(string(id), func(t *testing.T) {
			a := newTestAgent()
			definition, _ := mode.Resolve(id)
			if err := a.deps.toolExecutor.SetInteractionMode(definition.Snapshot); err != nil {
				t.Fatal(err)
			}
			model := &evidenceSequenceLLM{scripts: [][]types.StreamToken{
				{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "plan", Name: "todo_write", Arguments: `{"todos":[{"content":"compare alternatives","status":"in_progress"},{"content":"deliver recommendation","status":"pending"}]}`}}},
				{{Content: "Only the first step is done."}},
				{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "finish", Name: "todo_write", Arguments: `{"todos":[{"content":"compare alternatives","status":"completed","result":"compared reliability and implementation cost"},{"content":"deliver recommendation","status":"completed","result":"recommend the minimal shared checklist"}]}`}}},
				{{Content: "Here is the comparison and recommendation."}},
			}}
			a.deps.llmClient = model
			tool := &todo.TodoWriteTool{}
			tool.SetApplyFn(a.updateTodos)
			a.deps.toolRegistry.RegisterWithOptions(tool, tools.RegistrationOptions{CoordinatorAllowed: true, PlanAllowed: true})
			result := a.RunContext(mode.WithSnapshot(t.Context(), definition.Snapshot), "Compare the approaches and recommend one.", nil, nil)
			if result.Error != nil || model.calls != 4 || result.FinalOutput != "Here is the comparison and recommendation." {
				t.Fatalf("checklist ignored in %s: calls=%d result=%+v", id, model.calls, result)
			}
			snapshot := a.deps.ctxMgr.Snapshot()
			if snapshot.TodoGroup == "" || len(snapshot.TodoPlans[snapshot.TodoGroup]) != 2 {
				t.Fatal("mode did not persist its plan")
			}
		})
	}
}
func TestBlockedChecklistCanCloseWithVisibleReasons(t *testing.T) {
	a := newTestAgent()
	a.run.todoTouched = true
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{
		{Content: "run tests", Status: "blocked", Reason: "test service unavailable"},
		{Content: "publish", Status: "cancelled", Reason: "user cancelled publishing"},
	})
	reasoning := &reasoningResult{Action: actionChat, ActionInput: "Implementation is ready."}
	if !a.turnRunner.handleText(reasoning, nil) || a.run.err != nil {
		t.Fatal("blocked plan caused a continuation loop")
	}
	if !strings.Contains(a.run.finalText, "test service unavailable") || !strings.Contains(a.run.finalText, "user cancelled publishing") {
		t.Fatal("closeout hid unfinished steps")
	}
	if a.deps.ctxMgr.Status().TasksDone {
		t.Fatal("blocked/cancelled plan reported fully complete")
	}
}
func TestTodoSaveFailureIsNotPublishedAsSuccess(t *testing.T) {
	a := newTestAgent()
	calls := 0
	a.interaction.Todos = func([]protocol.TodoItem) { calls++ }
	a.deps.persistTodos = func() error { return fmt.Errorf("storage unavailable") }
	_, err := a.updateTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: "work", Status: "pending"}}})
	if err == nil || calls != 0 || a.deps.ctxMgr.Status().HasTasks {
		t.Fatal("failed persistence was acknowledged")
	}
}
func TestCompletionCannotResumeAnotherDemandPlan(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	a.deps.ctxMgr.SelectTodoGroup("other")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "other work", Status: "pending"}})
	a.deps.ctxMgr.SelectTodoGroup("demand")
	if _, err := a.updateTodos(protocol.TodoUpdate{PlanID: "other", SelectOnly: true}); err == nil {
		t.Fatal("completion switched into an unrelated demand")
	}
}

type todoActivityHost struct {
	pending int
	group   string
}

func (*todoActivityHost) SubmitBackground(context.Context, protocol.BackgroundWork) (protocol.BackgroundReceipt, error) {
	return protocol.BackgroundReceipt{}, nil
}
func (*todoActivityHost) CancelBackground(context.Context, string) error { return nil }
func (h *todoActivityHost) PendingBackgroundPlan(group string) int       { h.group = group; return h.pending }

func TestResumedPlanUsesCurrentBackgroundActivity(t *testing.T) {
	a := newTestAgent()
	a.deps.ctxMgr.SelectTodoGroup("saved")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "work", Status: "pending"}})
	host := &todoActivityHost{pending: 1}
	a.life.SetRunContext(protocol.WithBackgroundSubmitter(t.Context(), host))
	a.run.todoTouched = true
	if a.turnRunner.continueUnfinishedPlan() || host.group != "saved" {
		t.Fatal("active saved workers were ignored")
	}
	host.pending = 0
	if !a.turnRunner.continueUnfinishedPlan() {
		t.Fatal("idle unfinished plan was allowed to close")
	}
}
func TestResumingSavedPlanCannotEndAfterTextOnlyResponse(t *testing.T) {
	a := newTestAgent()
	a.deps.ctxMgr.SelectTodoGroup("saved")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "verify", Status: "pending"}})
	a.deps.llmClient = &evidenceSequenceLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "resume", Name: "todo_write", Arguments: `{"plan_id":"saved"}`}}},
		{{Content: "I will continue."}},
		{{Content: "I will continue."}},
		{{Content: "I will continue."}},
		{{Content: "I will continue."}},
	}}
	tool := &todo.TodoWriteTool{}
	tool.SetApplyFn(a.updateTodos)
	a.deps.toolRegistry.Register(tool)
	result := a.RunContext(t.Context(), "Continue the saved plan.", nil, nil)
	if result.Error != nil || result.StopReason != "incomplete" || !strings.Contains(result.FinalOutput, "verify") || !strings.Contains(result.FinalOutput, "I will continue.") {
		t.Fatalf("resumed plan silently ended: %+v", result)
	}
}

func TestTodoUpdateFailureCannotSilentlyCloseEmptyPlan(t *testing.T) {
	a := newTestAgent()
	a.deps.persistTodos = func() error { return fmt.Errorf("storage unavailable") }
	if _, err := a.updateTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: "work", Status: "pending"}}}); err == nil {
		t.Fatal("save succeeded unexpectedly")
	}
	for i := 0; i < 2; i++ {
		if a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "Report ready."}, nil) {
			t.Fatal("failed checklist update was ignored")
		}
	}
	if !a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "Report ready."}, nil) {
		t.Fatal("correction did not terminate")
	}
	if !strings.Contains(a.run.finalText, "清单同步失败") || !strings.Contains(a.run.finalText, "storage unavailable") || !strings.Contains(a.run.finalText, "Report ready.") {
		t.Fatalf("lost report or failure diagnostic: %q", a.run.finalText)
	}
}

func TestSuccessfulTodoUpdateClearsSyncFailure(t *testing.T) {
	a := newTestAgent()
	a.recordTodoUpdate(fmt.Errorf("previous error"))
	a.run.todoUpdateChecks = 2
	if _, err := a.updateTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: "work", Status: "completed", Result: "verified"}}}); err != nil {
		t.Fatal(err)
	}
	if a.run.todoUpdateError != "" || a.run.todoUpdateChecks != 0 {
		t.Fatal("successful reconciliation retained error")
	}
	a.run.todoTouched = true
	if !a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "Done."}, nil) || strings.Contains(a.run.finalText, "同步失败") {
		t.Fatal("successful plan blocked")
	}
}

func TestStandardRunRepairsFailedTodoTransactionBeforeClosing(t *testing.T) {
	a := newTestAgent()
	saves := 0
	a.deps.persistTodos = func() error {
		saves++
		if saves == 1 {
			return fmt.Errorf("temporary save failure")
		}
		return nil
	}
	model := &evidenceSequenceLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "first", Name: "todo_write", Arguments: `{"todos":[{"content":"verify","status":"completed","result":"verified"}]}`}}},
		{{Content: "Done."}},
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "retry", Name: "todo_write", Arguments: `{"todos":[{"content":"verify","status":"completed","result":"verified"}]}`}}},
		{{Content: "Verified and saved."}},
	}}
	a.deps.llmClient = model
	tool := &todo.TodoWriteTool{}
	tool.SetApplyFn(a.updateTodos)
	a.deps.toolRegistry.Register(tool)
	result := a.RunContext(t.Context(), "Verify the work.", nil, nil)
	if result.Error != nil || model.calls != 4 || saves != 2 || result.FinalOutput != "Verified and saved." || !a.deps.ctxMgr.Status().TasksDone {
		t.Fatalf("failed update was not reconciled: calls=%d saves=%d result=%+v", model.calls, saves, result)
	}
}

func TestTodoSyncFailureRetainsUnfinishedItemsInPartialReport(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	a.recordTodoUpdate(fmt.Errorf("disk unavailable"))
	a.run.todoUpdateChecks = 2
	if !a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "Inspection report."}, nil) {
		t.Fatal("partial closeout blocked indefinitely")
	}
	for _, want := range []string{"Inspection report.", "清单同步失败", "fix", "verify", "未完成"} {
		if !strings.Contains(a.run.finalText, want) {
			t.Fatalf("missing %q in %q", want, a.run.finalText)
		}
	}
}

func TestCompletionTodoErrorIdentifiesAllowedPlanAndRecovery(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	_, err := a.updateTodos(protocol.TodoUpdate{PlanID: "other", SelectOnly: true})
	if err == nil {
		t.Fatal("foreign plan accepted")
	}
	for _, hint := range []string{`"other"`, `"demand"`, "omit plan_id", "no changes"} {
		if !strings.Contains(err.Error(), hint) {
			t.Fatalf("missing recovery hint %q: %v", hint, err)
		}
	}
	if _, err := a.updateTodos(protocol.TodoUpdate{SelectOnly: true}); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if a.run.todoUpdateError != "" {
		t.Fatal("successful recovery retained sync failure")
	}
}

func TestCompletionTodoWithoutPlanIDUsesNotificationPlan(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	a.deps.ctxMgr.SelectTodoGroup("other")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "unrelated", Status: "pending"}})
	plan, err := a.updateTodos(protocol.TodoUpdate{Patch: true, Items: []protocol.TodoItem{{Content: "notification work", Status: "pending"}}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != "demand" {
		t.Fatalf("updated unrelated active plan: %s", plan.ID)
	}
	a.deps.ctxMgr.SelectTodoGroup("other")
	if got := a.deps.ctxMgr.Snapshot().Todos; len(got) != 1 || got[0].Content != "unrelated" {
		t.Fatalf("foreign checklist modified: %+v", got)
	}
}

func TestReopenedTodoDoesNotRenewCorrectionBudget(t *testing.T) {
	a := coordinatorWithTodos(t, 0)
	for i := 0; i < 4; i++ {
		items := a.deps.ctxMgr.Todos()
		if i%2 == 0 {
			items[0].Status = "completed"
		} else {
			items[0].Status = "pending"
		}
		a.deps.ctxMgr.SetTodos(items)
		if got := a.turnRunner.continueUnfinishedPlan(); got != (i < 3) {
			t.Fatalf("attempt %d continued=%v", i+1, got)
		}
	}
}

func TestEmptyResponseAfterToolResultsReportsUnfinishedWork(t *testing.T) {
	for _, summary := range []string{"The checklist was saved.", ""} {
		a := newTestAgent()
		tool := &todo.TodoWriteTool{}
		tool.SetApplyFn(a.updateTodos)
		a.deps.toolRegistry.Register(tool)
		model := &evidenceSequenceLLM{scripts: [][]types.StreamToken{
			{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "plan", Name: "todo_write", Arguments: `{"todos":[{"content":"verify changes","status":"pending"}]}`}}},
			{{ReasoningContent: "thinking"}, {Content: "  "}},
			{{Content: summary}},
		}}
		a.deps.llmClient = model
		result := a.Run("Plan and verify changes.", nil, nil)
		wantText := summary
		if wantText == "" {
			wantText = fallbackSynthesize
		}
		if result.Error != nil || result.StopReason != "incomplete" || model.calls != 3 ||
			!strings.Contains(result.FinalOutput, wantText) || !strings.Contains(result.FinalOutput, "verify changes（未完成）") {
			t.Fatalf("calls=%d result=%+v", model.calls, result)
		}
		if !containsMessage(model.messages, synthesizePrompt) || a.run.finalPersisted != (summary != "") {
			t.Fatalf("summary=%q persisted=%t messages=%+v", summary, a.run.finalPersisted, model.messages)
		}
	}
}
