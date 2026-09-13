package agent

import (
	"context"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/policy"
	"nekocode/bot/policy/builtin"
)

func newTestAgent() *Agent {
	ctxMgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "test", ContextWindow: 128000})
	reg := tools.New()
	gov := policy.New()
	builtin.Register(gov)
	agent := New(context.Background(), Config{
		Context: ctxMgr,
		Tools:   reg,
		Policy:  gov,
	})
	return agent
}

func TestHandleText_IsError_NotRecorded(t *testing.T) {
	a := newTestAgent()

	rr := &reasoningResult{
		Thought:     "LLM call failed",
		Action:      actionChat,
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

	rr := &reasoningResult{
		Thought:         "Format correction",
		Action:          actionChat,
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

	rr := &reasoningResult{
		Thought:     "Direct reply",
		Action:      actionChat,
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

	rr := &reasoningResult{
		Thought:     "LLM call failed",
		Action:      actionChat,
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

func TestStopHooksSetStructuredFinalIntent(t *testing.T) {
	a := newTestAgent()
	a.Reset()
	var intent policy.FinalIntent
	a.deps.gov.Register(policy.Hook{
		Name:  "capture-intent",
		Point: policy.Stop,
		On: func(s policy.State) *policy.Result {
			intent = s.Facts().Response.Intent
			return nil
		},
	})

	ok := &reasoningResult{
		Action:      actionChat,
		ActionInput: "done",
	}
	a.turnRunner.applyStopHooks(ok, isRecordableText(ok), nil)
	if intent != policy.FinalIntentFinal {
		t.Fatalf("final intent = %q, want final", intent)
	}

	errResult := &reasoningResult{
		Action:      actionChat,
		ActionInput: "LLM call failed",
		IsError:     true,
	}
	a.turnRunner.applyStopHooks(errResult, isRecordableText(errResult), nil)
	if intent != policy.FinalIntentError {
		t.Fatalf("error intent = %q, want error", intent)
	}

	garbled := &reasoningResult{
		Action:          actionChat,
		GarbledToolCall: true,
	}
	a.turnRunner.applyStopHooks(garbled, isRecordableText(garbled), nil)
	if intent != policy.FinalIntentFormatError {
		t.Fatalf("garbled intent = %q, want format_error", intent)
	}
}

func TestHandleText_NormalChat_ConsecutiveFailuresReset(t *testing.T) {
	a := newTestAgent()

	errRR := &reasoningResult{
		Thought:     "LLM call failed",
		Action:      actionChat,
		ActionInput: "error",
		IsError:     true,
	}
	a.turnRunner.handleText(errRR, nil)
	if a.run.consecutiveFailures != 1 {
		t.Fatalf("expected consecutiveFailures=1 after error, got %d", a.run.consecutiveFailures)
	}

	okRR := &reasoningResult{
		Thought:     "Direct reply",
		Action:      actionChat,
		ActionInput: "Hello!",
	}
	a.turnRunner.handleText(okRR, nil)
	if a.run.consecutiveFailures != 0 {
		t.Errorf("expected consecutiveFailures=0 after normal chat, got %d", a.run.consecutiveFailures)
	}
}

func TestPostToolStopClearsStaleFinalText(t *testing.T) {
	ctxMgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "test", ContextWindow: 128000})
	reg := tools.New()
	a := New(context.Background(), Config{Context: ctxMgr, Tools: reg})

	a.run.lastText = "previous text"
	a.run.finalText = "stale final"
	a.run.finalPersisted = true
	stop := policy.StopCompleted
	a.applyPostToolHookResult(policy.Result{Stop: &stop})

	if a.run.lastText != "" {
		t.Fatalf("lastText = %q, want cleared", a.run.lastText)
	}
	if a.run.finalText != "" {
		t.Fatalf("finalText = %q, want cleared", a.run.finalText)
	}
	if a.run.finalPersisted {
		t.Fatal("finalPersisted = true, want false")
	}
}

func TestPrepareTurnTreatsCanceledContextAsInterrupt(t *testing.T) {
	a := newTestAgent()
	// Simulates a steer/abort landing while auto-compaction would stream:
	// the turn context is already canceled when prepareTurn runs.
	a.life.Cancel()
	if err := a.turnRunner.prepareTurn(""); err != nil {
		t.Fatalf("canceled context should be treated as an interrupt, got run error: %v", err)
	}
}
