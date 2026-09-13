package contextmgr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

type controlledSummaryClient struct {
	usageTestClient
	tokens chan types.StreamToken
	errs   chan error
}

func (c *controlledSummaryClient) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	return c.tokens, c.errs
}
func (c *controlledSummaryClient) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	panic("compaction must stream")
}

func TestCompactionEmitsBeforeStreamFinishes(t *testing.T) {
	for _, mode := range []string{"manual", "auto", "cancel", "error"} {
		t.Run(mode, func(t *testing.T) {
			client := &controlledSummaryClient{tokens: make(chan types.StreamToken), errs: make(chan error, 1)}
			m := New(Config{CompactionModel: client, ContextWindow: 2000, AutoCompactPercent: 1})
			for range 8 {
				m.Add("user", "old question")
				m.Add("assistant", "old answer")
			}
			events := make(chan protocol.CompactionEvent, 20)
			m.SetCompactionObserver(func(event protocol.CompactionEvent) { _ = m.Status(); events <- event })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				if mode == "auto" {
					_, err = m.AutoCompactIfNeeded(ctx)
				} else {
					_, err = m.Summarize(ctx)
				}
				done <- err
			}()
			next := func() protocol.CompactionEvent {
				t.Helper()
				select {
				case e := <-events:
					return e
				case <-time.After(time.Second):
					t.Fatal("missing compaction event")
					return protocol.CompactionEvent{}
				}
			}
			start := next()
			if start.Status != "started" || start.ID == "" || start.BeforeMessages != 16 {
				t.Fatalf("start: %+v", start)
			}
			text := "<summary>Preserve the completed edits and continue with the remaining verification."
			client.tokens <- types.StreamToken{Content: text}
			delta := next()
			if delta.Delta != text || delta.ID != start.ID || delta.Status != "delta" {
				t.Fatalf("delta: %+v", delta)
			}
			select {
			case err := <-done:
				t.Fatalf("finished before stream closed: %v", err)
			default:
			}
			if mode == "cancel" {
				cancel()
				go func() {
					client.tokens <- types.StreamToken{Content: "discard cancelled output"}
					close(client.tokens)
					close(client.errs)
				}()
			} else {
				if mode == "error" {
					client.errs <- errors.New("stream failed")
				} else {
					client.tokens <- types.StreamToken{Content: "</summary>"}
					_ = next()
				}
				close(client.tokens)
				close(client.errs)
			}
			terminal := next()
			select {
			case err := <-done:
				if mode == "cancel" || mode == "error" {
					if err == nil || terminal.Status != "failed" || m.Status().Messages != 16 || terminal.Summary != text {
						t.Fatalf("failure: %+v, %v", terminal, err)
					}
				} else if err != nil || terminal.Status != "completed" || terminal.AfterMessages >= 16 || strings.Contains(terminal.Summary, "<summary>") {
					t.Fatalf("complete: %+v, %v", terminal, err)
				}
			case <-time.After(time.Second):
				t.Fatal("compaction did not settle")
			}
		})
	}
}

func TestNoCompactionDoesNotEmitDetails(t *testing.T) {
	m := New(Config{CompactionModel: &usageTestClient{}})
	m.Add("user", "short conversation")
	m.SetCompactionObserver(func(protocol.CompactionEvent) { t.Fatal("no-op emitted compaction") })
	if applied, err := m.Summarize(context.Background()); applied || err != nil {
		t.Fatalf("%v %v", applied, err)
	}
}

func TestBackupFailurePreventsCompactionCall(t *testing.T) {
	client := &controlledSummaryClient{tokens: make(chan types.StreamToken), errs: make(chan error)}
	m := New(Config{CompactionModel: client})
	for range 8 {
		m.Add("user", "old question")
		m.Add("assistant", "old answer")
	}
	m.SetBeforeCompaction(func(string) error { return errors.New("disk full") })
	if applied, err := m.Summarize(context.Background()); applied || err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Summarize = %v, %v", applied, err)
	}
}

// Compaction replaces the active history wholesale; index-based bookkeeping
// registered against the old list must be reset while the swap is settling.
func TestAfterCompactionHookFiresOnAppliedSwapOnly(t *testing.T) {
	newClient := func() *controlledSummaryClient {
		c := &controlledSummaryClient{tokens: make(chan types.StreamToken, 1), errs: make(chan error, 1)}
		c.tokens <- types.StreamToken{Content: "<summary>Conversation about finishing the migration.</summary>"}
		close(c.tokens)
		close(c.errs)
		return c
	}

	for _, tc := range []struct {
		name    string
		applyTo func(m *Manager)
	}{
		{"manual", func(m *Manager) { _, _ = m.Summarize(context.Background()) }},
		{"auto", func(m *Manager) { _, _ = m.AutoCompactIfNeeded(context.Background()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Config{CompactionModel: newClient(), ContextWindow: 2000, AutoCompactPercent: 1})
			for range 8 {
				m.Add("user", "old question with enough words to add tokens")
				m.Add("assistant", "old answer with enough words to add tokens")
			}
			called := 0
			m.SetAfterCompaction(func() { called++ })
			tc.applyTo(m)
			if called != 1 {
				t.Fatalf("afterCompaction called %d times, want 1", called)
			}
		})
	}

	// A failed compaction keeps the original history, so the hook must not fire.
	failing := &controlledSummaryClient{tokens: make(chan types.StreamToken), errs: make(chan error, 1)}
	failing.errs <- errors.New("summarizer down")
	close(failing.tokens)
	close(failing.errs)
	m := New(Config{CompactionModel: failing})
	for range 8 {
		m.Add("user", "old question")
		m.Add("assistant", "old answer")
	}
	called := 0
	m.SetAfterCompaction(func() { called++ })
	if _, err := m.Summarize(context.Background()); err == nil {
		t.Fatal("expected summarizer failure")
	}
	if called != 0 {
		t.Fatalf("afterCompaction fired on failed compaction (%d)", called)
	}
}

// Nothing to summarize: the hook must stay silent.
func TestAfterCompactionHookSkipsNoOp(t *testing.T) {
	m := New(Config{})
	m.Add("user", "short conversation")
	called := 0
	m.SetAfterCompaction(func() { called++ })
	if applied, err := m.Summarize(context.Background()); applied || err != nil {
		t.Fatalf("Summarize = %v, %v", applied, err)
	}
	if called != 0 {
		t.Fatalf("afterCompaction fired on no-op (%d)", called)
	}
}

func TestSummaryStreamMergesSplitUsage(t *testing.T) {
	client := &controlledSummaryClient{tokens: make(chan types.StreamToken, 3), errs: make(chan error)}
	client.tokens <- types.StreamToken{Usage: &types.StreamUsage{PromptTokens: 100, CacheHitTokens: 80, CacheMissTokens: 20, CacheUsageReported: true}}
	client.tokens <- types.StreamToken{Content: "summary"}
	client.tokens <- types.StreamToken{Usage: &types.StreamUsage{CompletionTokens: 20}}
	close(client.tokens)
	close(client.errs)
	m := New(Config{})
	var usage types.StreamUsage
	m.SetLLMUsageRecorder(func(u types.StreamUsage) { usage = u })
	if _, err := m.makeSummarizer(context.Background(), client)(nil, ""); err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 100 || usage.CompletionTokens != 20 || usage.CacheHitTokens != 80 || usage.TotalTokens != 120 {
		t.Fatalf("split usage lost: %+v", usage)
	}
}
