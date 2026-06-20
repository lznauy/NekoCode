package runtime

import (
	"strings"
	"testing"

	"nekocode/bot/agent/budget"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
)

func TestFilterToolCallsAppliesPreToolPolicyBlock(t *testing.T) {
	a := newTestAgent()
	a.gov.HookReg.Register(hooks.Hook{
		Name:  "block-read",
		Point: hooks.PreToolUse,
		On: func(s *hooks.Snapshot) *hooks.Result {
			return &hooks.Result{BlockTool: &hooks.BlockTool{
				Tool:   "read",
				Reason: "read blocked",
			}}
		},
	})

	filtered := a.filterToolCalls([]tools.ToolCallItem{
		{Name: "read", Args: map[string]any{"path": "x.go"}},
	}, &stepState{quota: budget.ToolQuota{MaxSlots: 8}})

	if len(filtered.allowed) != 0 {
		t.Fatalf("allowed = %d, want 0", len(filtered.allowed))
	}
	if got := filtered.blocked[0]; got != "read blocked" {
		t.Fatalf("blocked reason = %q, want read blocked", got)
	}
}

func TestEmitToolStartCallbacksMarksBlockedCalls(t *testing.T) {
	var events []string
	emitToolStartCallbacks([]tools.ToolCallItem{
		{Name: "read", Args: map[string]any{"path": "x.go"}},
		{Name: "write", Args: map[string]any{"path": "x.go"}},
	}, map[int]string{1: "blocked"}, func(action, toolName, toolArgs, output string) {
		events = append(events, action+":"+toolName)
	})

	got := strings.Join(events, ",")
	want := "tool_start:read,tool_blocked:write"
	if got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}
