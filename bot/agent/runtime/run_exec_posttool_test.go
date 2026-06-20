package runtime

import (
	"testing"

	"nekocode/bot/hooks"
)

func TestApplyPostToolHooksStopClearsLastText(t *testing.T) {
	a := newTestAgent()
	a.lastText = "previous"
	a.gov.HookReg.Register(hooks.Hook{
		Name:  "stop",
		Point: hooks.PostTool,
		On: func(s *hooks.Snapshot) *hooks.Result {
			stop := hooks.StopCompleted
			return &hooks.Result{Stop: &stop}
		},
	})

	shouldStop, reason := a.applyPostToolHooks()
	if !shouldStop {
		t.Fatal("expected PostTool stop")
	}
	if reason != hooks.StopCompleted {
		t.Fatalf("reason = %s, want completed", reason)
	}
	if a.lastText != "" {
		t.Fatalf("lastText = %q, want cleared", a.lastText)
	}
}
