package contextmgr

import (
	"testing"

	"nekocode/bot/provider/types"
)

// A reloaded process has no turn of its own yet, so /context must fall back to
// the turn recorded in the session instead of going blank.
func TestRestoredTurnReportedUntilLiveTurnStarts(t *testing.T) {
	request := ModelRequest{Tools: []types.ToolDef{{Type: "function", Function: types.FunctionDef{Name: "read"}}}}

	first := New(Config{SystemPrompt: "prompt"})
	first.Add("user", "hello")
	first.BuildRequest(request)
	first.RecordModelUsage(types.StreamUsage{
		PromptTokens: 1_000, CacheHitTokens: 800, CacheMissTokens: 200, CacheUsageReported: true,
	})

	snap := first.Snapshot()
	if snap.PrefixTurn == nil {
		t.Fatal("live turn was not captured")
	}
	if got := *snap.PrefixTurn; got.Requests != 1 || got.HitTokens != 800 || got.MissTokens != 200 || got.PeakMiss.MissTokens != 200 {
		t.Fatalf("persisted turn = %+v", got)
	}

	second := New(Config{SystemPrompt: "prompt"})
	second.Restore(snap)

	report := second.Report()
	if report.PrefixTurn.Requests != 0 {
		t.Fatalf("fresh process invented a live turn: %+v", report.PrefixTurn)
	}
	if got := report.SavedTurn; got.Requests != 1 || got.HitTokens != 800 || got.MissTokens != 200 {
		t.Fatalf("saved turn not reported = %+v", got)
	}

	// Saving before the first post-reload turn must not blank the slot.
	resaved := second.Snapshot()
	if resaved.PrefixTurn == nil || resaved.PrefixTurn.Requests != 1 {
		t.Fatalf("re-save lost the saved turn: %+v", resaved.PrefixTurn)
	}

	// This process's own turn takes over once it exists.
	second.BuildRequest(request)
	second.RecordModelUsage(types.StreamUsage{
		PromptTokens: 100, CacheHitTokens: 10, CacheMissTokens: 90, CacheUsageReported: true,
	})
	report = second.Report()
	if got := report.PrefixTurn; got.Requests != 1 || got.HitTokens != 10 || got.MissTokens != 90 {
		t.Fatalf("live turn = %+v", got)
	}
	if !report.SavedTurn.IsZero() {
		t.Fatalf("saved turn still reported alongside a live turn: %+v", report.SavedTurn)
	}
}

// A session with no recorded turn must not forge one.
func TestRestoreWithoutTurnReportsNothing(t *testing.T) {
	m := New(Config{SystemPrompt: "prompt"})
	m.Restore(ManagerSnapshot{SystemPrompt: "prompt"})
	if got := m.Report().SavedTurn; !got.IsZero() {
		t.Fatalf("saved turn = %+v, want zero", got)
	}
	if m.Snapshot().PrefixTurn != nil {
		t.Fatal("snapshot invented a turn")
	}
}
