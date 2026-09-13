package contextmgr

import (
	"testing"

	"nekocode/bot/contextmgr/token"
)

func TestSnapshotRestore(t *testing.T) {
	first := New(Config{SystemPrompt: "test prompt"})
	first.ConfigureModel(ModelContext{Window: 1_000_000})
	first.Add("user", "hello world")
	first.state.tracker.RecordPrompt(10_000)
	first.state.tracker.RecordCache(9_000, 1_000)
	first.state.tracker.RecordSubagent(300, 200, 100)
	first.state.compactCount = 2
	first.state.trimCount = 40

	snap := first.Snapshot()

	second := New(Config{SystemPrompt: "test prompt"})
	second.ConfigureModel(ModelContext{Window: 128_000})
	second.state.tracker.RecordSubagent(999, 999, 0)
	second.state.compactCount = 7
	second.Restore(snap)

	if got, want := len(second.Snapshot().Messages), len(first.Snapshot().Messages); got != want {
		t.Errorf("restored len = %d, want %d", got, want)
	}
	if got, want := len(second.Snapshot().Transcript), len(first.Snapshot().Transcript); got != want {
		t.Errorf("restored transcript len = %d, want %d", got, want)
	}
	if budget := second.Status().Budget; budget != 128_000 {
		t.Errorf("restored budget = %d, want active model budget 128000", budget)
	}
	tracker := second.state.tracker.Snapshot()
	// Cumulative cache hit/miss is a session-scoped diagnostic (it feeds the
	// /context "Session" line), so it survives a reload. The provider-calibrated
	// prompt count does not: the session may be resumed on another model.
	if tracker.CacheHitTokens != 9_000 || tracker.CacheMissTokens != 1_000 {
		t.Errorf("restored cache counters = %+v, want 9000/1000", tracker)
	}
	if tracker.LastPromptTokens != 0 {
		t.Errorf("restored prompt calibration = %d, want 0", tracker.LastPromptTokens)
	}
	if tracker.Sub != (token.SubStats{Count: 1, TotalTokens: 300, CacheHitTokens: 200, CacheMissTokens: 100}) {
		t.Errorf("restored sub-agent stats = %+v, want snapshot totals", tracker.Sub)
	}
	// Compaction counters describe the restored archive; zeroing them would
	// report an archive that no compaction produced.
	if got := second.Report(); got.CompactCount != 2 || got.Archived != 40 {
		t.Errorf("restored compaction counters = %d compactions / %d trimmed, want 2/40", got.CompactCount, got.Archived)
	}
}
