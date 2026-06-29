package compact

import (
	"testing"

	"nekocode/bot/contextmgr/context"
	"nekocode/bot/llm/types"
)

type testTracker struct{ promptEst, total int }

func (t *testTracker) PromptEstimate() int { return t.promptEst }
func (t *testTracker) Total() int          { return t.total }

func TestFindRecentTurnBoundary(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
	}
	cm := newCompactor(msgs, 64000, 0)

	if b := cm.findRecentTurnBoundary(1); b != 4 {
		t.Errorf("last 1 turn: got %d, want 4", b)
	}
	if b := cm.findRecentTurnBoundary(2); b != 2 {
		t.Errorf("last 2 turns: got %d, want 2", b)
	}
	if b := cm.findRecentTurnBoundary(99); b != 6 {
		t.Errorf("beyond all turns: got %d, want 6 (all messages eligible for compaction)", b)
	}
}

func TestEffectiveBudget(t *testing.T) {
	b128 := 128000
	b0 := 0
	if b := newCompactor(nil, b128, 0).effectiveBudget(); b != 128000 {
		t.Errorf("explicit = %d", b)
	}
	if b := newCompactor(nil, b0, 0).effectiveBudget(); b != 64000 {
		t.Errorf("default = %d, want 64000", b)
	}
}

func TestEffectiveConfig(t *testing.T) {
	cfg := newCompactor(nil, 64000, 0).effectiveConfig()
	if cfg.WarningBuffer != DefaultConfig.WarningBuffer {
		t.Error("default budget should use unscaled config")
	}

	cfg2 := newCompactor(nil, 128000, 0).effectiveConfig()
	if cfg2.WarningBuffer <= DefaultConfig.WarningBuffer {
		t.Error("larger budget should scale config upward")
	}
}

func TestVisibleEstimatedTokens(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "hello world"}, {Role: "assistant", Content: "reply"},
		{Role: "user", Content: "ignored"}, {Role: "assistant", Content: "also ignored"},
	}
	budget := 64000
	cm := &Compactor{
		Ctx:           &context.Content{Messages: msgs, CompactBoundary: 2, Archive: "summary text"},
		ContextWindow: &budget, Tracker: &testTracker{},
		CompactCount: new(int), TrimCount: new(int), Cfg: DefaultConfig,
	}
	n := cm.visibleEstimatedTokens()
	if n <= 8 {
		t.Errorf("with 2 visible msgs + archive: got %d, want > 8", n)
	}
}

func TestNeedsSummarization(t *testing.T) {
	cm := newCompactor(nil, 64000, 0)
	if cm.NeedsSummarization() {
		t.Error("without summarizer → false")
	}

	cm.Summarizer = func(msgs []types.Message, prev string) (string, error) { return "ok", nil }
	if cm.NeedsSummarization() {
		t.Error("≤ 20 messages → false")
	}
}
