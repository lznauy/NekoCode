package toolrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	aggov "nekocode/bot/policy"
	"nekocode/bot/policy/budget"
	"nekocode/bot/tools/core"
	"nekocode/bot/tools"
	"nekocode/bot/tools/runner"
)

type fakeHost struct {
	ctx        context.Context
	ctxMgr     *ctxmgr.Manager
	executor   *runner.Executor
	gov        *aggov.Manager
	subSlots   *SlotManager
	step       int
	stopReason hooks.StopReason
	lastText   string
	hints      []*hooks.Hint
}

func newFakeHost() *fakeHost {
	hookReg := hooks.NewRegistry()
	hooks.RegisterBuiltin(hookReg)
	return &fakeHost{
		ctx:      context.Background(),
		ctxMgr:   ctxmgr.NewSub("test", 128000, nil),
		executor: runner.NewExecutor(tools.NewRegistry()),
		gov:      aggov.NewManager(hookReg),
		subSlots: NewSlotManager(),
	}
}

func (h *fakeHost) Context() context.Context             { return h.ctx }
func (h *fakeHost) ContextManager() *ctxmgr.Manager      { return h.ctxMgr }
func (h *fakeHost) Executor() *runner.Executor            { return h.executor }
func (h *fakeHost) Governance() *aggov.Manager           { return h.gov }
func (h *fakeHost) SubSlots() *SlotManager               { return h.subSlots }
func (h *fakeHost) InjectHint(hint *hooks.Hint)          { h.hints = append(h.hints, hint) }
func (h *fakeHost) IncStep()                             { h.step++ }
func (h *fakeHost) StopPostTool(reason hooks.StopReason) { h.stopReason = reason; h.lastText = "" }

func TestFilterToolCallsAppliesPreToolPolicyBlock(t *testing.T) {
	host := newFakeHost()
	host.gov.HookReg.Register(hooks.Hook{
		Name:  "block-read",
		Point: hooks.PreToolUse,
		On: func(s *hooks.Snapshot) *hooks.Result {
			return &hooks.Result{BlockTool: &hooks.BlockTool{
				Tool:   "read",
				Reason: "read blocked",
			}}
		},
	})

	filtered := New(host).FilterToolCalls([]core.ToolCallItem{
		{Name: "read", Args: map[string]any{"path": "x.go"}},
	}, &budget.ToolQuota{MaxSlots: 8})

	if len(filtered.Allowed) != 0 {
		t.Fatalf("allowed = %d, want 0", len(filtered.Allowed))
	}
	if got := filtered.Blocked[0]; got != "read blocked" {
		t.Fatalf("blocked reason = %q, want read blocked", got)
	}
}

func TestFilterToolCallsReadBeforeWriteBlockComesFromHook(t *testing.T) {
	host := newFakeHost()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	filtered := New(host).FilterToolCalls([]core.ToolCallItem{
		{Name: "write", Args: map[string]any{"path": path}},
	}, &budget.ToolQuota{MaxSlots: 8})

	if len(filtered.Allowed) != 0 {
		t.Fatalf("allowed = %d, want 0", len(filtered.Allowed))
	}
	if got := filtered.Blocked[0]; !strings.Contains(got, "ledger 中没有该文件的读取记录") {
		t.Fatalf("blocked reason = %q, want read-before-write hook reason", got)
	}
}

func TestApplyPostToolHooksStopClearsLastText(t *testing.T) {
	host := newFakeHost()
	host.lastText = "previous"
	host.gov.HookReg.Register(hooks.Hook{
		Name:  "stop",
		Point: hooks.PostTool,
		On: func(s *hooks.Snapshot) *hooks.Result {
			stop := hooks.StopCompleted
			return &hooks.Result{Stop: &stop}
		},
	})

	if !New(host).ApplyPostToolHooks() {
		t.Fatal("expected PostTool stop")
	}
	if host.stopReason != hooks.StopCompleted {
		t.Fatalf("reason = %s, want completed", host.stopReason)
	}
	if host.lastText != "" {
		t.Fatalf("lastText = %q, want cleared", host.lastText)
	}
}
