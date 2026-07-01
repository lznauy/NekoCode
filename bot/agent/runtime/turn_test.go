package runtime

import (
	"strings"
	"testing"

	"nekocode/bot/hooks"
)

func TestHandleText_IsError_NotRecorded(t *testing.T) {
	a := newTestAgent()

	rr := &ReasoningResult{
		Thought:     "LLM call failed",
		Action:      ActionChat,
		ActionInput: "LLM call failed: connection refused",
		IsError:     true,
	}

	msgCountBefore := a.deps.ctxMgr.Len()
	finished := a.turnRunner.handleText(rr, nil)

	if !finished {
		t.Error("expected finished=true for IsError without hook hints")
	}
	if a.deps.ctxMgr.Len() != msgCountBefore {
		t.Errorf("expected no messages added to context, got %d (was %d)",
			a.deps.ctxMgr.Len(), msgCountBefore)
	}
	if a.run.consecutiveFailures != 1 {
		t.Errorf("expected consecutiveFailures=1, got %d", a.run.consecutiveFailures)
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

	msgCountBefore := a.deps.ctxMgr.Len()
	finished := a.turnRunner.handleText(rr, nil)

	if !finished {
		t.Error("expected finished=true for GarbledToolCall without hook hints")
	}
	if a.deps.ctxMgr.Len() != msgCountBefore {
		t.Errorf("expected no messages added to context, got %d (was %d)",
			a.deps.ctxMgr.Len(), msgCountBefore)
	}
}

func TestHandleText_NormalChat_Recorded(t *testing.T) {
	a := newTestAgent()

	rr := &ReasoningResult{
		Thought:     "Direct reply",
		Action:      ActionChat,
		ActionInput: "Hello, world!",
	}

	msgCountBefore := a.deps.ctxMgr.Len()
	finished := a.turnRunner.handleText(rr, nil)

	if !finished {
		t.Error("expected finished=true for normal chat")
	}
	if a.deps.ctxMgr.Len() != msgCountBefore+1 {
		t.Errorf("expected 1 message added to context, got %d (was %d)",
			a.deps.ctxMgr.Len(), msgCountBefore)
	}
	if a.run.consecutiveFailures != 0 {
		t.Errorf("expected consecutiveFailures=0, got %d", a.run.consecutiveFailures)
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

	for i := 1; i <= maxConsecutiveFailures; i++ {
		finished := a.turnRunner.handleText(rr, nil)
		if !finished {
			t.Errorf("step %d: expected finished=true", i)
		}
		if a.run.consecutiveFailures != i {
			t.Errorf("step %d: expected consecutiveFailures=%d, got %d",
				i, i, a.run.consecutiveFailures)
		}
	}

	finished := a.turnRunner.handleText(rr, nil)
	if !finished {
		t.Error("expected finished=true after limit reached")
	}
	if a.run.consecutiveFailures != 6 {
		t.Errorf("expected consecutiveFailures=6, got %d", a.run.consecutiveFailures)
	}
}

func TestHandleText_IsError_WithPendingTasks_HintInjected(t *testing.T) {
	a := newTestAgent()

	a.deps.gov.HookReg.Set(hooks.StoreHasTasks, 1)
	a.deps.gov.HookReg.Set(hooks.StoreTasksAllDone, 0)
	a.deps.gov.HookReg.Set(hooks.StoreTurnToolCalls, 0)

	rr := &ReasoningResult{
		Thought:     "LLM call failed",
		Action:      ActionChat,
		ActionInput: "LLM call failed: connection refused",
		IsError:     true,
	}

	msgCountBefore := a.deps.ctxMgr.Len()
	finished := a.turnRunner.handleText(rr, nil)

	if finished {
		t.Error("expected finished=false when PostTurn hook injects hint")
	}
	added := a.deps.ctxMgr.Len() - msgCountBefore
	if added != 0 {
		t.Errorf("expected hint to stay out of history, got %d messages added", added)
	}

	a.applyTurnHints(nil)
	msgs := a.deps.ctxMgr.Build()
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "system" || !strings.Contains(msgs[len(msgs)-1].Content, `type="policy_block"`) {
		t.Fatalf("expected pending hook hint in transient system layer, got %+v", msgs)
	}
}

func TestHandleText_NormalChat_ConsecutiveFailuresReset(t *testing.T) {
	a := newTestAgent()

	errRR := &ReasoningResult{
		Thought:     "LLM call failed",
		Action:      ActionChat,
		ActionInput: "error",
		IsError:     true,
	}
	a.turnRunner.handleText(errRR, nil)
	if a.run.consecutiveFailures != 1 {
		t.Fatalf("expected consecutiveFailures=1 after error, got %d", a.run.consecutiveFailures)
	}

	okRR := &ReasoningResult{
		Thought:     "Direct reply",
		Action:      ActionChat,
		ActionInput: "Hello!",
	}
	a.turnRunner.handleText(okRR, nil)
	if a.run.consecutiveFailures != 0 {
		t.Errorf("expected consecutiveFailures=0 after normal chat, got %d", a.run.consecutiveFailures)
	}
}
