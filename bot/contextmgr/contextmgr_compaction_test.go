package contextmgr

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestAutoCompactIfNeeded_NoStrategy(t *testing.T) {
	m := New(Config{SystemPrompt: "test prompt"})
	m.state.compressor = nil
	if _, err := m.AutoCompactIfNeeded(context.Background()); err != nil {
		t.Errorf("AutoCompactIfNeeded error: %v", err)
	}
}

func TestAutoCompactReportsActualOverflowWhenNothingCanBeTrimmed(t *testing.T) {
	m := New(Config{SystemPrompt: strings.Repeat("system context ", 20), ContextWindow: 1})
	compacted, err := m.AutoCompactIfNeeded(context.Background())
	if compacted || err == nil || !strings.Contains(err.Error(), "context full") {
		t.Fatalf("AutoCompactIfNeeded() = %v, %v; want untrimmed context full", compacted, err)
	}
}

func TestAppliedCompactionRunsAfterHookWhenStillOverBudget(t *testing.T) {
	m := New(Config{
		SystemPrompt:  strings.Repeat("system context ", 20),
		ContextWindow: 1,
		Summarizer: func([]types.Message, string) (string, error) {
			return "<summary>Compacted context summary that remains over the deliberately tiny budget.</summary>", nil
		},
	})
	for i := 0; i < 8; i++ {
		m.Add("user", "old question")
		m.Add("assistant", "old answer")
	}
	afterCalls := 0
	m.SetAfterCompaction(func() { afterCalls++ })

	applied, err := m.AutoCompactIfNeeded(context.Background())
	if !applied || err == nil || !strings.Contains(err.Error(), "context full") {
		t.Fatalf("AutoCompactIfNeeded() = %v, %v; want applied compaction with overflow warning", applied, err)
	}
	if afterCalls != 1 {
		t.Fatalf("after-compaction hook calls = %d, want 1 after history replacement", afterCalls)
	}
}

func TestSummarizeDoesNotBlockOrOverwriteConcurrentHistory(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	m := New(Config{
		SystemPrompt: "system",
		Summarizer: func(msgs []types.Message, prev string) (string, error) {
			close(started)
			<-release
			return "<summary>Compacted context summary that is long enough to pass validation.</summary>", nil
		},
	})
	for i := 0; i < 8; i++ {
		m.Add("user", "old question")
		m.AddAssistant(types.Message{Content: "old answer"})
	}

	compactDone := make(chan error, 1)
	go func() {
		_, err := m.Summarize(context.Background())
		compactDone <- err
	}()
	<-started

	addDone := make(chan struct{})
	go func() {
		m.Add("user", "message added during summary")
		close(addDone)
	}()
	select {
	case <-addDone:
	case <-time.After(time.Second):
		t.Fatal("Add blocked while summarizer was running")
	}

	close(release)
	if err := <-compactDone; err == nil || !strings.Contains(err.Error(), "context changed") {
		t.Fatalf("Summarize() error = %v, want stale-summary rejection", err)
	}
	if !containsContent(m.Build(), "message added during summary") {
		t.Fatal("concurrent message was overwritten by stale summary")
	}
}

func TestManagerSummarizeUsesDefaultCompressionStrategy(t *testing.T) {
	m := New(Config{
		SystemPrompt: "system",
		Summarizer: func(msgs []types.Message, prev string) (string, error) {
			return "<summary>Compacted context summary that replaces the hidden full conversation history.</summary>", nil
		},
	})
	for i := 1; i <= 8; i++ {
		m.Add("user", "old question")
		m.Add("assistant", "old answer")
	}

	if compacted, err := m.Summarize(context.Background()); err != nil {
		t.Fatalf("Summarize() error: %v", err)
	} else if !compacted {
		t.Fatal("Summarize() did not compact")
	}

	snap := m.Snapshot()
	if len(snap.Messages) >= 16 {
		t.Fatalf("messages were not replaced: got %d", len(snap.Messages))
	}
	if len(snap.Transcript) != 16 {
		t.Fatalf("compaction changed permanent transcript: got %d messages", len(snap.Transcript))
	}
	if report := m.Report(); report.CompactCount != 1 || report.Archived == 0 || report.TrimCount != report.Archived {
		t.Fatalf("compaction counters = %+v", report)
	}

	exported := m.Build()
	joined := joinContents(exported)
	if !strings.Contains(joined, "Compacted context summary") {
		t.Fatalf("exported context missing archive: %q", joined)
	}
}

func joinContents(msgs []types.Message) string {
	var b strings.Builder
	for _, msg := range msgs {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

// Regression: compact used to install its per-invocation event wrapper back
// into compressor.summarizer. With an injected (non-model) summarizer, every
// later compaction then wrapped the previous wrapper again and replayed its
// stale CompactionStarted event, so the observer received more Started events
// than there were compactions.
func TestRepeatedCompactionEmitsOneStartedEventEach(t *testing.T) {
	m := New(Config{
		SystemPrompt: "system",
		Summarizer: func(msgs []types.Message, prev string) (string, error) {
			return "<summary>Compacted context summary that replaces the hidden full conversation history.</summary>", nil
		},
	})
	var mu sync.Mutex
	var startedIDs []string
	m.SetCompactionObserver(func(ev protocol.CompactionEvent) {
		mu.Lock()
		defer mu.Unlock()
		if ev.Status == protocol.CompactionStarted {
			startedIDs = append(startedIDs, ev.ID)
		}
	})

	for round := 0; round < 2; round++ {
		for i := 1; i <= 8; i++ {
			m.Add("user", fmt.Sprintf("round %d old question %d", round, i))
			m.Add("assistant", "old answer")
		}
		if _, err := m.Summarize(context.Background()); err != nil {
			t.Fatalf("Summarize() round %d error: %v", round, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(startedIDs) != 2 {
		t.Fatalf("got %d CompactionStarted events for 2 compactions, want exactly one each: %v", len(startedIDs), startedIDs)
	}
	if startedIDs[0] == startedIDs[1] {
		t.Fatalf("both Started events carry the same compaction id %q", startedIDs[0])
	}
}
