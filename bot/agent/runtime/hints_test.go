package runtime

import (
	"testing"

	"nekocode/bot/hooks"
)

func TestInjectHintUsesTransientLayerOnly(t *testing.T) {
	a := newTestAgent()
	before := a.deps.ctxMgr.Len()

	a.injectHint(&hooks.Hint{Type: "final_check", Severity: "critical", Content: "run verification"})
	if got := a.deps.ctxMgr.Len(); got != before {
		t.Fatalf("hint changed history length: got %d, want %d", got, before)
	}

	a.applyTurnHints(nil)
	msgs := a.deps.ctxMgr.Build(false)
	if !messagesContain(msgs, `type="final_check"`) || !messagesContain(msgs, "run verification") {
		t.Fatalf("expected transient final_check hint in build messages, got %+v", msgs)
	}

	a.deps.ctxMgr.SetHints("")
	msgs = a.deps.ctxMgr.Build(false)
	if messagesContain(msgs, `type="final_check"`) {
		t.Fatalf("final_check hint leaked after clearing transient hints: %+v", msgs)
	}
	if got := a.deps.ctxMgr.Len(); got != before {
		t.Fatalf("hint leaked into history length: got %d, want %d", got, before)
	}
}
