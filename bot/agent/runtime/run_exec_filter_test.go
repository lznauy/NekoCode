package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/hooks"
	"nekocode/bot/policy/budget"
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

func TestFilterToolCallsReadBeforeWriteBlockComesFromHook(t *testing.T) {
	a := newTestAgent()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	filtered := a.filterToolCalls([]tools.ToolCallItem{
		{Name: "write", Args: map[string]any{"path": path}},
	}, &stepState{quota: budget.ToolQuota{MaxSlots: 8}})

	if len(filtered.allowed) != 0 {
		t.Fatalf("allowed = %d, want 0", len(filtered.allowed))
	}
	if got := filtered.blocked[0]; !strings.Contains(got, "ledger 中没有该文件的读取记录") {
		t.Fatalf("blocked reason = %q, want read-before-write hook reason", got)
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
