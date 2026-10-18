package telegram

import (
	"context"
	"strings"
	"time"

	"nekocode/interaction/connect/telegram/internal/taskview"
	controlruntime "nekocode/runtime"
)

// Streaming preview tuning (Telegram limits: ~1 edit/sec per chat, 4096 chars).
const (
	previewMinStart     = 300                     // runes buffered before the preview is created (short replies skip previewing entirely)
	previewEditInterval = 1200 * time.Millisecond // min time between in-place edits
	previewEditGrowth   = 200                     // min new runes before an edit is worth sending
	previewMaxChunk     = 3000                    // raw runes per message; escaping may grow it, parse failures fall back to plain text
)

// previewSender is the subset of apiClient the preview tracker needs.
type previewSender interface {
	sendMessageID(ctx context.Context, chatID int64, text string) (int, error)
	editMessageText(ctx context.Context, chatID int64, messageID int, text string) error
	sendMessage(ctx context.Context, chatID int64, text string) error
}

// previewTracker accumulates assistant text deltas into a single editable
// preview message per chat (OpenClaw-style partial streaming): the preview is
// created once enough text exists, edited in place on a throttle, and settled
// with the final text when the run ends. Short/fast replies never create a
// preview — the caller's normal done-reply handles those (debounce).
type previewTracker struct {
	sender  previewSender
	chatIDs func() []int64
	now     func() time.Time

	runID         controlruntime.RunID
	buf           []rune
	msgIDs        map[int64]int
	hasPreview    bool
	lastFlush     time.Time
	sentLen       int
	responseStart int
}

func newPreviewTracker(sender previewSender, chatIDs func() []int64) *previewTracker {
	return &previewTracker{sender: sender, chatIDs: chatIDs, now: time.Now}
}

// addDelta buffers one assistant delta and flushes if due.
func (p *previewTracker) addDelta(ctx context.Context, runID controlruntime.RunID, delta string) {
	if runID != p.runID {
		p.reset(runID)
	}
	p.buf = append(p.buf, []rune(delta)...)
	p.flush(ctx)
}

func (p *previewTracker) retract(ctx context.Context, runID controlruntime.RunID) {
	if p.runID != runID {
		return
	}
	p.buf = p.buf[:min(p.responseStart, len(p.buf))]
	if !p.hasPreview {
		return
	}
	text := taskview.MarkdownToHTML(string(p.buf))
	if text == "" {
		text = "正在处理…"
	}
	for chatID, id := range p.msgIDs {
		_ = p.sender.editMessageText(ctx, chatID, id, text)
	}
	p.sentLen, p.lastFlush = len(p.buf), p.now()
}

// flush creates the preview once the buffer is big enough, then edits it in
// place whenever both the interval and growth thresholds are met.
func (p *previewTracker) flush(ctx context.Context) {
	if len(p.buf) == 0 {
		return
	}
	if !p.hasPreview {
		if len(p.buf) < previewMinStart {
			return
		}
		p.msgIDs = make(map[int64]int, len(p.chatIDs()))
		for _, chatID := range p.chatIDs() {
			if id, err := p.sender.sendMessageID(ctx, chatID, taskview.MarkdownToHTML(string(p.buf))); err == nil {
				p.msgIDs[chatID] = id
			}
		}
		p.hasPreview = true
		p.sentLen = len(p.buf)
		p.lastFlush = p.now()
		return
	}
	if p.now().Sub(p.lastFlush) < previewEditInterval || len(p.buf)-p.sentLen < previewEditGrowth {
		return
	}
	for chatID, id := range p.msgIDs {
		_ = p.sender.editMessageText(ctx, chatID, id, taskview.MarkdownToHTML(string(p.buf)))
	}
	p.sentLen = len(p.buf)
	p.lastFlush = p.now()
}

// finalize settles the preview with the full buffered text (chunked: the
// preview keeps the first chunk, the rest goes out as new messages). It
// reports whether a preview existed — when false, the caller should deliver
// the final text through its normal done-reply path instead.
func (p *previewTracker) finalize(ctx context.Context, runID controlruntime.RunID) bool {
	existed := p.hasPreview
	if !existed {
		p.reset(runID)
		return false
	}
	chunks := chunkText(string(p.buf), previewMaxChunk)
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	for chatID, id := range p.msgIDs {
		_ = p.sender.editMessageText(ctx, chatID, id, taskview.MarkdownToHTML(chunks[0]))
		for _, chunk := range chunks[1:] {
			_ = p.sender.sendMessage(ctx, chatID, taskview.MarkdownToHTML(chunk))
		}
	}
	p.reset(runID)
	return true
}

// drop discards any in-flight preview state without settling it.
func (p *previewTracker) reset(runID controlruntime.RunID) {
	p.runID = runID
	p.buf = nil
	p.msgIDs = nil
	p.hasPreview = false
	p.sentLen = 0
	p.responseStart = 0
	p.lastFlush = time.Time{}
}

// chunkText splits s into chunks of at most max runes, preferring paragraph
// then line boundaries in the back half of the window.
func chunkText(s string, max int) []string {
	var chunks []string
	rest := s
	for {
		runes := []rune(rest)
		if len(runes) <= max {
			if len(runes) > 0 {
				chunks = append(chunks, rest)
			}
			return chunks
		}
		window := string(runes[:max])
		cut := strings.LastIndex(window, "\n\n")
		if cut < max/2 {
			cut = strings.LastIndex(window, "\n")
		}
		if cut < max/2 {
			cut = len(window)
		}
		chunk := strings.TrimRight(window[:cut], "\n")
		if chunk == "" {
			chunk = window
		}
		chunks = append(chunks, chunk)
		rest = strings.TrimLeft(string([]rune(rest)[len([]rune(chunk)):]), "\n")
	}
}
