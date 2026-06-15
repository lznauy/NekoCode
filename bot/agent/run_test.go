package agent

import (
	"context"
	"testing"

	"nekocode/bot/ctxmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
)

func newTestAgent() *Agent {
	ctxMgr := ctxmgr.NewSub("test", 128000, nil)
	reg := tools.NewRegistry()
	a := New(context.Background(), ctxMgr, nil, reg)
	a.hookReg = hooks.NewRegistry()
	hooks.RegisterBuiltin(a.hookReg)
	return a
}

func TestHandleText_IsError_NotRecorded(t *testing.T) {
	a := newTestAgent()

	rr := &ReasoningResult{
		Thought:     "LLM call failed",
		Action:      ActionChat,
		ActionInput: "LLM call failed: connection refused",
		IsError:     true,
	}

	msgCountBefore := a.ctxMgr.Len()
	finished := a.handleText(rr, &stepState{}, nil)

	if !finished {
		t.Error("expected finished=true for IsError without hook hints")
	}
	if a.ctxMgr.Len() != msgCountBefore {
		t.Errorf("expected no messages added to context, got %d (was %d)",
			a.ctxMgr.Len(), msgCountBefore)
	}
	if a.consecutiveFailures != 1 {
		t.Errorf("expected consecutiveFailures=1, got %d", a.consecutiveFailures)
	}
}

func TestHandleText_GarbledToolCall_NotRecorded(t *testing.T) {
	a := newTestAgent()

	rr := &ReasoningResult{
		Thought:         "Format correction",
		Action:          ActionChat,
		ActionInput:     "",
		GarbledToolCall: true,
	}

	msgCountBefore := a.ctxMgr.Len()
	finished := a.handleText(rr, &stepState{}, nil)

	if !finished {
		t.Error("expected finished=true for GarbledToolCall without hook hints")
	}
	if a.ctxMgr.Len() != msgCountBefore {
		t.Errorf("expected no messages added to context, got %d (was %d)",
			a.ctxMgr.Len(), msgCountBefore)
	}
}

func TestHandleText_NormalChat_Recorded(t *testing.T) {
	a := newTestAgent()

	rr := &ReasoningResult{
		Thought:     "Direct reply",
		Action:      ActionChat,
		ActionInput: "Hello, world!",
	}

	msgCountBefore := a.ctxMgr.Len()
	finished := a.handleText(rr, &stepState{}, nil)

	if !finished {
		t.Error("expected finished=true for normal chat")
	}
	if a.ctxMgr.Len() != msgCountBefore+1 {
		t.Errorf("expected 1 message added to context, got %d (was %d)",
			a.ctxMgr.Len(), msgCountBefore)
	}
	if a.consecutiveFailures != 0 {
		t.Errorf("expected consecutiveFailures=0, got %d", a.consecutiveFailures)
	}
}

func TestHandleText_IsError_ConsecutiveFailuresIncrement(t *testing.T) {
	a := newTestAgent()

	rr := &ReasoningResult{
		Thought:     "LLM call failed",
		Action:      ActionChat,
		ActionInput: "LLM call failed: timeout",
		IsError:     true,
	}

	// Each IsError call increments consecutiveFailures and finishes
	// (no hook hints to keep the loop alive).
	for i := 1; i <= maxConsecutiveFailures; i++ {
		finished := a.handleText(rr, &stepState{}, nil)
		if !finished {
			t.Errorf("step %d: expected finished=true", i)
		}
		if a.consecutiveFailures != i {
			t.Errorf("step %d: expected consecutiveFailures=%d, got %d",
				i, i, a.consecutiveFailures)
		}
	}

	// After reaching the limit, the next call stops with the limit message.
	finished := a.handleText(rr, &stepState{}, nil)
	if !finished {
		t.Error("expected finished=true after limit reached")
	}
	// consecutiveFailures is now 6 (5 increments + 1 that triggered the stop).
	if a.consecutiveFailures != 6 {
		t.Errorf("expected consecutiveFailures=6, got %d", a.consecutiveFailures)
	}
}

func TestHandleText_IsError_WithPendingTasks_HintInjected(t *testing.T) {
	a := newTestAgent()

	a.hookReg.Set(hooks.StoreHasTasks, 1)
	a.hookReg.Set(hooks.StoreTasksAllDone, 0)
	a.hookReg.Set(hooks.StoreTurnToolCalls, 0)

	rr := &ReasoningResult{
		Thought:     "LLM call failed",
		Action:      ActionChat,
		ActionInput: "LLM call failed: connection refused",
		IsError:     true,
	}

	msgCountBefore := a.ctxMgr.Len()
	finished := a.handleText(rr, &stepState{}, nil)

	if finished {
		t.Error("expected finished=false when PostTurn hook injects hint")
	}
	// Only the hint (system message) should be added, not the error as assistant.
	added := a.ctxMgr.Len() - msgCountBefore
	if added != 1 {
		t.Errorf("expected 1 message (hint) added, got %d", added)
	}
}

func TestHandleText_NormalChat_ConsecutiveFailuresReset(t *testing.T) {
	a := newTestAgent()

	// First, trigger an error to bump the counter.
	errRR := &ReasoningResult{
		Thought:     "LLM call failed",
		Action:      ActionChat,
		ActionInput: "error",
		IsError:     true,
	}
	a.handleText(errRR, &stepState{}, nil)
	if a.consecutiveFailures != 1 {
		t.Fatalf("expected consecutiveFailures=1 after error, got %d", a.consecutiveFailures)
	}

	// Then a normal chat should reset it.
	okRR := &ReasoningResult{
		Thought:     "Direct reply",
		Action:      ActionChat,
		ActionInput: "Hello!",
	}
	a.handleText(okRR, &stepState{}, nil)
	if a.consecutiveFailures != 0 {
		t.Errorf("expected consecutiveFailures=0 after normal chat, got %d", a.consecutiveFailures)
	}
}
