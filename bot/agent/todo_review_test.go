package agent

import (
	"nekocode/bot/policy"
	"nekocode/protocol"
	"testing"
)

func TestTodoReviewCrossPlanDispatch(t *testing.T) {
	a := newTestAgent()
	a.deps.ctxMgr.SelectTodoGroup("A")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "A work", Status: "pending"}})
	a.run.todoDispatched = true
	a.run.todoDispatchPlan = "A"
	a.deps.ctxMgr.SelectTodoGroup("B")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "B work", Status: "pending"}})
	a.run.todoTouched = true
	finished := a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "B is complete"}, nil)
	if finished && a.run.finalText == "B is complete" {
		t.Fatal("BUG: dispatch for A permits unfinished B to close")
	}
}
func TestTodoReviewStopCompletedSkipsTodo(t *testing.T) {
	a := newTestAgent()
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "not delivered", Status: "pending"}})
	a.run.todoTouched = true
	_, finished := a.turnRunner.applyStopOutcome(policy.Outcome{Decision: policy.Decision{Action: policy.Terminate, StopReason: policy.StopCompleted}}, &reasoningResult{Action: actionChat, ActionInput: "All done"}, true, nil)
	if finished && a.run.finalText == "All done" {
		t.Fatal("BUG: StopCompleted commits full completion with actionable todo")
	}
}
func TestTodoReviewHintCapHidesBlocked(t *testing.T) {
	a := newTestAgent()
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "verify", Status: "blocked", Reason: "CI offline"}})
	a.run.todoTouched = true
	a.run.consecutiveHints = maxConsecutiveHints - 1
	done := a.turnRunner.applyStopHint(&reasoningResult{Action: actionChat, ActionInput: "All done"}, &policy.Hint{Type: "advice", Content: "check"}, true, nil)
	if done && a.run.finalText == "All done" {
		t.Fatal("BUG: hint-cap closeout omits blocked reason")
	}
}

func TestFinishedWorkerDoesNotPermitUnfinishedChecklistToClose(t *testing.T) {
	a := newTestAgent()
	a.deps.ctxMgr.SelectTodoGroup("A")
	a.deps.ctxMgr.SetTodos([]protocol.TodoItem{{Content: "verification", Status: "pending"}})
	a.run.todoTouched = true
	a.run.todoDispatched = true
	a.run.todoDispatchPlan = "A"
	host := &todoActivityHost{pending: 0}
	a.life.SetValues(protocol.WithBackgroundSubmitter(t.Context(), host))
	if !a.turnRunner.continueUnfinishedPlan() {
		t.Fatal("stale dispatch flag bypassed live idle status")
	}
}
