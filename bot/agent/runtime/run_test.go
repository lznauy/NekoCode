package runtime

import (
	"context"
	"os"
	"strings"
	"testing"

	aggov "nekocode/bot/agent/governance"
	"nekocode/bot/agent/governance/ledger"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/governance"
	"nekocode/bot/hooks"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
)

func newTestAgent() *Agent {
	ctxMgr := ctxmgr.NewSub("test", 128000, nil)
	reg := tools.NewRegistry()
	a := New(context.Background(), ctxMgr, nil, reg)
	a.gov = aggov.NewManager(hooks.NewRegistry())
	hooks.RegisterBuiltin(a.gov.HookReg)
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

	a.gov.HookReg.Set(hooks.StoreHasTasks, 1)
	a.gov.HookReg.Set(hooks.StoreTasksAllDone, 0)
	a.gov.HookReg.Set(hooks.StoreTurnToolCalls, 0)

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
	added := a.ctxMgr.Len() - msgCountBefore
	if added != 0 {
		t.Errorf("expected hint to stay out of history, got %d messages added", added)
	}

	a.applyTurnHints(nil)
	msgs := a.ctxMgr.Build(false)
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "system" || !strings.Contains(msgs[len(msgs)-1].Content, `type="policy_block"`) {
		t.Fatalf("expected pending hook hint in transient system layer, got %+v", msgs)
	}
}

func TestInjectHintUsesTransientLayerOnly(t *testing.T) {
	a := newTestAgent()
	before := a.ctxMgr.Len()

	a.injectHint(&hooks.Hint{Type: "final_check", Severity: "critical", Content: "run verification"})
	if got := a.ctxMgr.Len(); got != before {
		t.Fatalf("hint changed history length: got %d, want %d", got, before)
	}

	a.applyTurnHints(nil)
	msgs := a.ctxMgr.Build(false)
	if !messagesContain(msgs, `type="final_check"`) || !messagesContain(msgs, "run verification") {
		t.Fatalf("expected transient final_check hint in build messages, got %+v", msgs)
	}

	a.ctxMgr.SetHints("")
	msgs = a.ctxMgr.Build(false)
	if messagesContain(msgs, `type="final_check"`) {
		t.Fatalf("final_check hint leaked after clearing transient hints: %+v", msgs)
	}
	if got := a.ctxMgr.Len(); got != before {
		t.Fatalf("hint leaked into history length: got %d, want %d", got, before)
	}
}

func messagesContain(msgs []types.Message, substr string) bool {
	for _, msg := range msgs {
		if strings.Contains(msg.Content, substr) {
			return true
		}
	}
	return false
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

func TestPreEditBlockReasonRequiresLedgerReadForExistingFile(t *testing.T) {
	a := newTestAgent()
	path := t.TempDir() + "/target.go"
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := tools.ToolCallItem{Name: "write", Args: map[string]any{"path": path}}
	if got := a.preEditBlockReason(tc); got == "" {
		t.Fatal("expected existing unread file to be blocked")
	}

	a.gov.Ledger.RecordTool(ledger.ToolEvent{
		Name:      "read",
		Args:      map[string]any{"path": path},
		Semantics: governance.ClassifyToolCall("read", map[string]any{"path": path}),
	})
	if got := a.preEditBlockReason(tc); got != "" {
		t.Fatalf("expected read file to pass, got %q", got)
	}
}

func TestPreEditBlockReasonAllowsEditWithSufficientAnchor(t *testing.T) {
	a := newTestAgent()
	path := t.TempDir() + "/target.go"
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tmessage := \"hello\"\n\tprintln(message)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := tools.ToolCallItem{Name: "edit", Args: map[string]any{
		"path": path,
		"oldString": strings.Join([]string{
			"package main",
			"",
			"func main() {",
			"\tmessage := \"hello\"",
			"\tprintln(message)",
			"}",
		}, "\n"),
		"newString": "package main\n",
	}}
	if got := a.preEditBlockReason(tc); got != "" {
		t.Fatalf("expected sufficiently anchored edit to pass without read, got %q", got)
	}
}

func TestPreEditBlockReasonBlocksEditWithShortAnchor(t *testing.T) {
	a := newTestAgent()
	path := t.TempDir() + "/target.go"
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := tools.ToolCallItem{Name: "edit", Args: map[string]any{
		"path":      path,
		"oldString": "main",
		"newString": "app",
	}}
	if got := a.preEditBlockReason(tc); got == "" {
		t.Fatal("expected short unread edit to be blocked")
	}
}

func TestPreEditBlockReasonAllowsNewFile(t *testing.T) {
	a := newTestAgent()
	path := t.TempDir() + "/new.go"
	tc := tools.ToolCallItem{Name: "write", Args: map[string]any{"path": path}}
	if got := a.preEditBlockReason(tc); got != "" {
		t.Fatalf("expected new file write to pass, got %q", got)
	}
}
