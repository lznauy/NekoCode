package policy

import (
	"testing"

	"nekocode/bot/hooks"
)

func TestGovRecordToolCallUpdatesResearcherAndMutationHooks(t *testing.T) {
	g := NewManager(hooks.NewRegistry())
	g.HookReg.Register(hooks.Hook{
		Name:  "assert-recorded",
		Point: hooks.PreTurn,
		On: func(s *hooks.Snapshot) *hooks.Result {
			if s.Store[hooks.StoreToolResearcher] != 1 {
				t.Fatalf("researcher count = %d, want 1", s.Store[hooks.StoreToolResearcher])
			}
			if s.Store[hooks.StoreHasEdits] != 1 {
				t.Fatalf("has edits = %d, want 1", s.Store[hooks.StoreHasEdits])
			}
			if s.Store[hooks.StoreTurnToolCalls] != 2 {
				t.Fatalf("turn tool calls = %d, want 2", s.Store[hooks.StoreTurnToolCalls])
			}
			return nil
		},
	})

	g.RecordToolCall(ToolCallInfo{
		Name: "task",
		Args: map[string]any{"type": "researcher"},
	}, false, "")
	g.RecordToolCall(ToolCallInfo{
		Name: "write",
		Args: map[string]any{"path": "x.go"},
	}, false, "")

	g.HookReg.Evaluate(hooks.PreTurn, "", false)
}
