package feishu

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"nekocode/interaction/connect"
)

type fakeSender struct {
	cards   []map[string]any
	texts   []string
	images  []string
	cardErr error
}

func (f *fakeSender) sendImage(_ context.Context, _ string, path string) error {
	f.images = append(f.images, path)
	return nil
}

func (f *fakeSender) sendCard(_ context.Context, _ string, card map[string]any) error {
	if f.cardErr != nil {
		return f.cardErr
	}
	f.cards = append(f.cards, card)
	return nil
}

func (f *fakeSender) sendText(_ context.Context, _ string, text string) error {
	f.texts = append(f.texts, text)
	return nil
}

func markdownOf(t *testing.T, card map[string]any) string {
	t.Helper()
	if card["schema"] != "2.0" {
		t.Fatalf("schema = %v, want 2.0", card["schema"])
	}
	if cfg, ok := card["config"].(map[string]any); !ok || cfg["wide_screen_mode"] != true {
		t.Fatalf("config = %v, want wide_screen_mode", card["config"])
	}
	elements, ok := card["body"].(map[string]any)["elements"].([]any)
	if !ok || len(elements) != 1 {
		t.Fatalf("body.elements = %v, want exactly one element", card["body"])
	}
	el, ok := elements[0].(map[string]any)
	if !ok || el["tag"] != "markdown" {
		t.Fatalf("element = %v, want a markdown component", elements[0])
	}
	content, _ := el["content"].(string)
	return content
}

func TestMarkdownCardStructure(t *testing.T) {
	if got := markdownOf(t, markdownCard("# 标题\n\n- a")); got != "# 标题\n\n- a" {
		t.Fatalf("content = %q", got)
	}
}

func resultSink(c *Connector, sender *fakeSender) eventSink {
	return eventSink{c: c, client: sender}
}

// TestPostResultSendsReferencedImages covers the outbound image path: a
// result intent referencing local image files must deliver them as native
// image messages before the markdown card.
func TestPostResultSendsReferencedImages(t *testing.T) {
	c := pairedConnector(t, &stubRuntime{})
	sender := &fakeSender{}

	intent := connect.Intent{
		Kind:   connect.IntentResult,
		RunID:  "run_1",
		Text:   "图表已生成 /tmp/charts/a.png 和 /tmp/charts/b.png",
		Images: []string{"/tmp/charts/a.png", "/tmp/charts/b.png"},
	}
	if err := resultSink(c, sender).Post(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if len(sender.images) != 2 || sender.images[0] != "/tmp/charts/a.png" || sender.images[1] != "/tmp/charts/b.png" {
		t.Fatalf("images = %v, want both referenced files in order", sender.images)
	}
	if len(sender.cards) != 1 {
		t.Fatalf("cards = %d, want the markdown card after the images", len(sender.cards))
	}

	// Failed-run intents never carry images.
	sender.images = nil
	if err := resultSink(c, sender).Post(context.Background(), connect.Intent{
		Kind: connect.IntentFailed, RunID: "run_2", Text: "boom",
		Images: []string{"/tmp/x.png"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(sender.images) != 0 {
		t.Fatalf("failed intents must not send images, got %v", sender.images)
	}
}

func TestPostResultSendsMarkdownCard(t *testing.T) {
	c := pairedConnector(t, &stubRuntime{})
	sender := &fakeSender{}
	text := "# 结果\n\n1. one\n2. two"
	if err := resultSink(c, sender).Post(context.Background(), connect.Intent{Kind: connect.IntentResult, Text: text}); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("unexpected plain-text sends: %v", sender.texts)
	}
	if len(sender.cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(sender.cards))
	}
	if got := markdownOf(t, sender.cards[0]); got != text {
		t.Fatalf("card content = %q, want the original markdown", got)
	}
}

func TestPostFailedSendsMarkdownCard(t *testing.T) {
	c := pairedConnector(t, &stubRuntime{})
	sender := &fakeSender{}
	text := "运行失败: boom"
	if err := resultSink(c, sender).Post(context.Background(), connect.Intent{Kind: connect.IntentFailed, Text: text}); err != nil {
		t.Fatal(err)
	}
	if len(sender.cards) != 1 || len(sender.texts) != 0 {
		t.Fatalf("cards = %d texts = %d, want 1 card and no text", len(sender.cards), len(sender.texts))
	}
	if got := markdownOf(t, sender.cards[0]); got != text {
		t.Fatalf("card content = %q", got)
	}
}

func TestPostResultFallsBackToPlainText(t *testing.T) {
	c := pairedConnector(t, &stubRuntime{})
	sender := &fakeSender{cardErr: fmt.Errorf("code=230001 card not supported")}
	text := "# 结果\n\n**bold**"
	if err := resultSink(c, sender).Post(context.Background(), connect.Intent{Kind: connect.IntentResult, Text: text}); err != nil {
		t.Fatal(err)
	}
	if len(sender.cards) != 0 {
		t.Fatalf("failed card should not be recorded: %v", sender.cards)
	}
	if len(sender.texts) != 1 || sender.texts[0] != text {
		t.Fatalf("fallback texts = %v, want the original text", sender.texts)
	}
}

func TestPostResultTruncatesLongContent(t *testing.T) {
	c := pairedConnector(t, &stubRuntime{})
	sender := &fakeSender{}
	long := strings.Repeat("猫", maxMarkdownRunes+500)
	if err := resultSink(c, sender).Post(context.Background(), connect.Intent{Kind: connect.IntentResult, Text: long}); err != nil {
		t.Fatal(err)
	}
	if len(sender.cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(sender.cards))
	}
	got := markdownOf(t, sender.cards[0])
	if r := []rune(got); len(r) != maxMarkdownRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated content = %d runes, want %d + ellipsis", len(r), maxMarkdownRunes)
	}
}

func TestPostStoppedStaysPlainText(t *testing.T) {
	c := pairedConnector(t, &stubRuntime{})
	sender := &fakeSender{}
	if err := resultSink(c, sender).Post(context.Background(), connect.Intent{Kind: connect.IntentStopped, Text: "已停止"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.cards) != 0 {
		t.Fatalf("stopped intent should not send cards: %v", sender.cards)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "已停止" {
		t.Fatalf("texts = %v", sender.texts)
	}
}
