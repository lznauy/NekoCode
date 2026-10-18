package telegram

import (
	"context"
	"fmt"
	"nekocode/interaction/connect"
	"strings"
	"testing"
	"time"

	sharedhttp "nekocode/util/http"
)

type stubMsg struct {
	chatID int
	id     int
	text   string
}

type stubSender struct {
	sent  []stubMsg
	edits []stubMsg
}

func (s *stubSender) sendMessageID(_ context.Context, chatID int64, text string) (int, error) {
	id := len(s.sent) + 1
	s.sent = append(s.sent, stubMsg{chatID: int(chatID), id: id, text: text})
	return id, nil
}

func (s *stubSender) editMessageText(_ context.Context, chatID int64, messageID int, text string) error {
	s.edits = append(s.edits, stubMsg{chatID: int(chatID), id: messageID, text: text})
	return nil
}

func (s *stubSender) sendMessage(_ context.Context, chatID int64, text string) error {
	s.sent = append(s.sent, stubMsg{chatID: int(chatID), text: text})
	return nil
}

func newTestPreview(sender *stubSender, now *time.Time) *previewTracker {
	p := newPreviewTracker(sender, func() []int64 { return []int64{42} })
	p.now = func() time.Time { return *now }
	return p
}

func TestPreviewShortReplyNeverCreatesPreview(t *testing.T) {
	sender := &stubSender{}
	now := time.Now()
	p := newTestPreview(sender, &now)

	p.addDelta(context.Background(), "run1", "短回复")
	if p.hasPreview {
		t.Fatal("preview created for short reply")
	}
	if p.finalize(context.Background(), "run1") {
		t.Fatal("finalize should report no preview")
	}
	if len(sender.sent) != 0 || len(sender.edits) != 0 {
		t.Fatalf("unexpected traffic: sent=%d edits=%d", len(sender.sent), len(sender.edits))
	}
}

func TestPreviewCreatedAfterThreshold(t *testing.T) {
	sender := &stubSender{}
	now := time.Now()
	p := newTestPreview(sender, &now)

	p.addDelta(context.Background(), "run1", strings.Repeat("a", previewMinStart))
	if !p.hasPreview {
		t.Fatal("preview not created at threshold")
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d, want 1 preview message", len(sender.sent))
	}
}

func TestPreviewResetRetractsOnlyCurrentResponseAndUsesCanonicalResult(t *testing.T) {
	sender := &stubSender{}
	now := time.Now()
	p := newTestPreview(sender, &now)
	p.addDelta(context.Background(), "run1", "keep")
	p.responseStart = len(p.buf) // native tool boundary
	p.addDelta(context.Background(), "run1", strings.Repeat("injected", 100))
	p.retract(context.Background(), "run1")
	if got := string(p.buf); got != "keep" {
		t.Fatalf("remaining text = %q", got)
	}
	if got := sender.edits[len(sender.edits)-1].text; got != "keep" {
		t.Fatalf("preview not retracted: %q", got)
	}
	sink := &eventSink{preview: p}
	if err := sink.Post(context.Background(), connect.Intent{Kind: connect.IntentResult, RunID: "run1", Text: "canonical answer"}); err != nil {
		t.Fatal(err)
	}
	if got := sender.edits[len(sender.edits)-1].text; got != "canonical answer" {
		t.Fatalf("final preview = %q", got)
	}
}

func TestPreviewEditThrottle(t *testing.T) {
	sender := &stubSender{}
	start := time.Now()
	now := start
	p := newTestPreview(sender, &now)

	p.addDelta(context.Background(), "run1", strings.Repeat("a", previewMinStart))

	// Growth below threshold: no edit even with time passing.
	now = start.Add(3 * time.Second)
	p.addDelta(context.Background(), "run1", "bc")
	p.flush(context.Background())
	if len(sender.edits) != 0 {
		t.Fatalf("edits = %d, want 0 (growth too small)", len(sender.edits))
	}

	// Enough growth but interval not elapsed: still no edit.
	now = start.Add(500 * time.Millisecond)
	p.addDelta(context.Background(), "run1", strings.Repeat("d", previewEditGrowth))
	if len(sender.edits) != 0 {
		t.Fatalf("edits = %d, want 0 (interval not elapsed)", len(sender.edits))
	}

	// Both thresholds met: exactly one edit.
	now = start.Add(2 * time.Second)
	p.flush(context.Background())
	if len(sender.edits) != 1 {
		t.Fatalf("edits = %d, want 1", len(sender.edits))
	}
}

func TestPreviewFinalizeChunksLongText(t *testing.T) {
	sender := &stubSender{}
	now := time.Now()
	p := newTestPreview(sender, &now)

	p.addDelta(context.Background(), "run1", strings.Repeat("a", previewMinStart))
	long := strings.Repeat("段落一的内容。\n\n", 400) // ~ 4000+ runes
	p.addDelta(context.Background(), "run1", long)

	if !p.finalize(context.Background(), "run1") {
		t.Fatal("finalize should report a preview existed")
	}
	if len(sender.edits) != 1 {
		t.Fatalf("edits = %d, want 1 (preview settled in place)", len(sender.edits))
	}
	if len(sender.sent) < 2 {
		t.Fatalf("sent = %d, want preview + at least 1 continuation message", len(sender.sent))
	}
	for i, m := range sender.sent[1:] {
		if len([]rune(m.text)) > previewMaxChunk*2 {
			t.Fatalf("continuation %d too long: %d runes", i, len([]rune(m.text)))
		}
	}
}

func TestPreviewRunSwitchResets(t *testing.T) {
	sender := &stubSender{}
	now := time.Now()
	p := newTestPreview(sender, &now)

	p.addDelta(context.Background(), "run1", strings.Repeat("a", previewMinStart))
	p.addDelta(context.Background(), "run2", "新 run 的短文本")
	if p.runID != "run2" {
		t.Fatalf("runID = %q, want run2", p.runID)
	}
	if p.hasPreview {
		t.Fatal("preview state should reset on run switch")
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d, want only run1's preview", len(sender.sent))
	}
}

func TestChunkText(t *testing.T) {
	short := "短文本"
	if chunks := chunkText(short, 100); len(chunks) != 1 || chunks[0] != short {
		t.Fatalf("short text chunks = %v", chunks)
	}

	// Long text: every chunk within limit, content preserved.
	paragraphs := make([]string, 50)
	for i := range paragraphs {
		paragraphs[i] = fmt.Sprintf("第%d段的内容，包含一些文字。", i)
	}
	full := strings.Join(paragraphs, "\n\n")
	chunks := chunkText(full, 200)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	var joined string
	for i, c := range chunks {
		if len([]rune(c)) > 200 {
			t.Fatalf("chunk %d exceeds limit: %d runes", i, len([]rune(c)))
		}
		joined += c
		if i < len(chunks)-1 {
			joined += "\n\n"
		}
	}
	if !strings.Contains(joined, "第49段") {
		t.Fatal("tail paragraph lost in chunking")
	}
}

func TestIsParseFailure(t *testing.T) {
	if isParseFailure(nil) {
		t.Fatal("nil is not a parse failure")
	}
	if !isParseFailure(sharedhttp.NewHTTPError(400, `{"description":"Bad Request: can't parse entities"}`)) {
		t.Fatal("HTTP 400 should be a parse failure")
	}
	if isParseFailure(sharedhttp.NewHTTPError(500, "server error")) {
		t.Fatal("HTTP 500 is not a parse failure")
	}
	if !isParseFailure(fmt.Errorf("telegram api: Bad Request: can't parse entities")) {
		t.Fatal("API-level parse error should match")
	}
	if isParseFailure(fmt.Errorf("network timeout")) {
		t.Fatal("network error is not a parse failure")
	}
}
