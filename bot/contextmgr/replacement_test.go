package contextmgr

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
)

func TestSummarizeReplacesHistoryWithArchiveAndRecentMessages(t *testing.T) {
	ctx := newContextContent("system")
	for i := 1; i <= 8; i++ {
		ctx.Messages = append(ctx.Messages,
			types.Message{Role: "user", Content: "question " + string(rune('0'+i))},
			types.Message{Role: "assistant", Content: "answer " + string(rune('0'+i))},
		)
	}
	budget := 64000
	s := newReplacementCompactor(func(msgs []types.Message, prev string) (string, error) {
		if len(msgs) == 0 {
			t.Fatal("expected messages to summarize")
		}
		return "<summary>This is a compacted project summary with enough detail to pass the quality floor.</summary>", nil
	}, 0)

	archive, recent, trimmed, err := s.summarize(ctx.Messages, "", budget, nil)
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}
	if len(recent) >= 16 {
		t.Fatalf("messages were not replaced: got %d", len(recent))
	}
	if !strings.Contains(archive, "compacted project summary") {
		t.Fatalf("archive not updated: %q", archive)
	}
	if recent[0].Content != "question 6" {
		t.Fatalf("first kept message = %q, want question 6", recent[0].Content)
	}
	if trimmed != 10 {
		t.Fatalf("trimmed = %d, want 10", trimmed)
	}
}

func TestRecentStartIgnoresInternalRuntimeUserMessages(t *testing.T) {
	var history []types.Message
	for i := 1; i <= 8; i++ {
		history = append(history,
			types.Message{Role: "user", Content: fmt.Sprintf("question %d", i)},
			types.Message{Role: "user", Content: "runtime", Source: types.MessageSourceRuntimeContext},
			types.Message{Role: "assistant", Content: fmt.Sprintf("answer %d", i)},
		)
	}
	start := userTurnBoundary(history, 3)
	if got := history[start].Content; got != "question 6" {
		t.Fatalf("runtime snapshots counted as user turns: boundary=%d content=%q", start, got)
	}
}

func TestRuntimeEventIsSummaryEvidenceNotAUserTurn(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "question 1"},
		{Role: "assistant", Content: "answer 1"},
		{Role: "user", Content: "question 2"},
		{Role: "user", Content: "rewind", Source: types.MessageSourceRuntimeEvent},
		{Role: "assistant", Content: "answer 2"},
		{Role: "user", Content: "question 3"},
	}
	if start := userTurnBoundary(history, 3); start != 0 {
		t.Fatalf("runtime event counted as a user turn: boundary=%d", start)
	}
	filtered := withoutInternalContext(history)
	if !slices.ContainsFunc(filtered, func(msg types.Message) bool {
		return msg.Source == types.MessageSourceRuntimeEvent
	}) {
		t.Fatal("runtime event was removed from summary evidence")
	}
}

func TestCompactionDoesNotArchiveRuntimeSnapshots(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "old request"},
		{Role: "user", Content: "old runtime", Source: types.MessageSourceRuntimeContext},
		{Role: "assistant", Content: "old answer"},
	}
	filtered := withoutInternalContext(messages)
	if len(filtered) != 2 || filtered[0].Content != "old request" || filtered[1].Content != "old answer" {
		t.Fatalf("summarizable history retained runtime state: %+v", filtered)
	}
}

func TestCompactionKeepsOnlyLatestRuntimeSnapshotInActiveEpoch(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "request"},
		{Role: "user", Content: "runtime one", Source: types.MessageSourceRuntimeContext},
		{Role: "assistant", Content: "working"},
		{Role: "user", Content: "runtime two", Source: types.MessageSourceRuntimeContext},
	}
	recent := retainLatestRuntimeContext(messages)
	if len(recent) != 3 || recent[len(recent)-1].Content != "runtime two" {
		t.Fatalf("runtime snapshots were not collapsed to latest: %+v", recent)
	}
}

func TestCompactionDropsTransientHintsWithoutRuntimeSnapshot(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "request"},
		{Role: "user", Content: "hint", Source: types.MessageSourceHint},
		{Role: "assistant", Content: "working"},
	}
	recent := retainLatestRuntimeContext(messages)
	if len(recent) != 2 || recent[0].Content != "request" || recent[1].Content != "working" {
		t.Fatalf("transient hint survived compaction cleanup: %+v", recent)
	}
}

func TestCompactionThresholdScalesWithContextWindow(t *testing.T) {
	compactor := newReplacementCompactor(nil, 0)
	if got := compactor.compactionThreshold(64_000); got != 51_200 {
		t.Fatalf("64k compaction threshold = %d, want 51200", got)
	}
	if got := compactor.compactionThreshold(1_000_000); got != 800_000 {
		t.Fatalf("1m compaction threshold = %d, want 800000", got)
	}

	compactor = newReplacementCompactor(nil, 65)
	if got := compactor.compactionThreshold(1_000_000); got != 650_000 {
		t.Fatalf("custom compaction threshold = %d, want 650000", got)
	}
}
