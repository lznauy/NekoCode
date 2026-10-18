package session

import (
	"testing"

	"nekocode/bot/contextmgr"
	"nekocode/bot/policy/ledger"
	"nekocode/bot/provider/types"
)

func TestManagerSnapshotsAreDetachedAtEveryBoundary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(t.TempDir())
	s := m.Current()
	s.Messages = []types.Message{{Role: "user", Content: "original", ToolCalls: []types.ToolCall{{ID: "call"}}, Images: []types.MessageImage{{Path: "image"}}}}
	s.Transcript = s.Messages
	s.LoadedSkills = []string{"skill"}
	s.CheckpointTurns = []string{"turn"}
	s.PrefixTurn = &contextmgr.PrefixTurnStats{PeakMiss: contextmgr.PrefixCallStats{Parts: []string{"system"}}}
	s.Ledger = ledger.Snapshot{ReadFiles: []string{"read"}, ModifiedFiles: []string{"write"}, BlockedTools: []ledger.ToolEvent{{Args: map[string]any{"nested": []any{map[string]any{"path": "original"}}}}}}
	if err := m.Activate(s); err != nil {
		t.Fatal(err)
	}
	mutate := func(s *Snapshot) {
		s.Messages[0].ToolCalls[0].ID = "changed"
		s.Messages[0].Images[0].Path = "changed"
		s.Transcript[0].Content = "changed"
		s.LoadedSkills[0] = "changed"
		s.CheckpointTurns[0] = "changed"
		s.PrefixTurn.PeakMiss.Parts[0] = "changed"
		s.Ledger.ReadFiles[0] = "changed"
		s.Ledger.ModifiedFiles[0] = "changed"
		s.Ledger.BlockedTools[0].Args["nested"].([]any)[0].(map[string]any)["path"] = "changed"
	}
	mutate(s)
	mutate(m.Current())
	got := m.Current()
	if got.Messages[0].ToolCalls[0].ID != "call" || got.Messages[0].Images[0].Path != "image" || got.Transcript[0].Content != "original" || got.LoadedSkills[0] != "skill" || got.CheckpointTurns[0] != "turn" || got.PrefixTurn.PeakMiss.Parts[0] != "system" || got.Ledger.ReadFiles[0] != "read" || got.Ledger.ModifiedFiles[0] != "write" || got.Ledger.BlockedTools[0].Args["nested"].([]any)[0].(map[string]any)["path"] != "original" {
		t.Fatalf("snapshot alias: %+v", got)
	}
	if err := m.Save(got); err != nil {
		t.Fatal(err)
	}
	mutate(got)
	if m.Current().Transcript[0].Content != "original" {
		t.Fatal("Save retained caller memory")
	}
	fresh := m.StartNew()
	fresh.ID = "changed"
	if m.CurrentID() == "changed" {
		t.Fatal("StartNew retained caller memory")
	}
}

func TestCloneToolArgsCopiesTypedCompositesAndPreservesNil(t *testing.T) {
	type typedItem struct{ Labels []string }
	args := map[string]any{
		"nil_map": map[string]any(nil),
		"typed":   []map[string]typedItem{{"item": {Labels: []string{"original"}}}},
	}
	cloned := cloneValue(args).(map[string]any)
	if cloned["nil_map"].(map[string]any) != nil {
		t.Fatal("nil map lost nil identity")
	}
	args["typed"].([]map[string]typedItem)[0]["item"].Labels[0] = "changed"
	if got := cloned["typed"].([]map[string]typedItem)[0]["item"].Labels[0]; got != "original" {
		t.Fatalf("typed composite retained alias: %q", got)
	}
}
