package subagent

import (
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/tools/core"
)

func TestApplyReadOnlySpiralGuardInjectsReminderAfterThreeExploratoryBatches(t *testing.T) {
	ctxMgr := ctxmgr.NewSub("system", 128000, nil)
	state := newRunState()
	calls := []core.ToolCallItem{{Name: "read", Args: map[string]any{"path": "a.go"}}}

	before := ctxMgr.Len()
	applyReadOnlySpiralGuard(ctxMgr, calls, state)
	applyReadOnlySpiralGuard(ctxMgr, calls, state)
	if ctxMgr.Len() != before {
		t.Fatalf("reminder injected too early: len=%d before=%d", ctxMgr.Len(), before)
	}

	applyReadOnlySpiralGuard(ctxMgr, calls, state)
	if ctxMgr.Len() != before+1 {
		t.Fatalf("len=%d, want reminder after third read-only batch", ctxMgr.Len())
	}
	if state.readOnlyStreak != 0 {
		t.Fatalf("readOnlyStreak = %d, want reset", state.readOnlyStreak)
	}
}

func TestApplyReadOnlySpiralGuardResetsOnMutation(t *testing.T) {
	ctxMgr := ctxmgr.NewSub("system", 128000, nil)
	state := &runState{readOnlyStreak: 2}
	applyReadOnlySpiralGuard(ctxMgr, []core.ToolCallItem{{Name: "write", Args: map[string]any{"path": "a.go"}}}, state)
	if state.readOnlyStreak != 0 {
		t.Fatalf("readOnlyStreak = %d, want reset", state.readOnlyStreak)
	}
}
