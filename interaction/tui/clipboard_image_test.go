package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestReadBoundedRejectsOversizedClipboardPayload(t *testing.T) {
	data, tooLarge, err := readBounded(strings.NewReader("123456"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if !tooLarge || data != nil {
		t.Fatalf("readBounded = (%q, %v), want oversized rejection", data, tooLarge)
	}
}

func TestReadBoundedReturnsPayloadWithinLimit(t *testing.T) {
	data, tooLarge, err := readBounded(strings.NewReader("12345"), 5)
	if err != nil || tooLarge || string(data) != "12345" {
		t.Fatalf("readBounded = (%q, %v, %v)", data, tooLarge, err)
	}
}

func TestClipboardImageIsRejectedWhileBrowsingHistory(t *testing.T) {
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.Input.SetHistory([]string{"older"})
	m.Input.HistoryUp()

	m.handleClipboardImage(clipboardImageMsg{data: []byte("image")})

	if len(m.Messages.Items()) != 1 {
		t.Fatalf("message count = %d, want history warning", len(m.Messages.Items()))
	}
	got := ansi.Strip(m.Messages.Items()[0].Render(100))
	if !strings.Contains(got, "历史") || !strings.Contains(got, "图片") {
		t.Fatalf("history warning = %q", got)
	}
	if len(m.Input.ImageAttachments()) != 0 {
		t.Fatalf("history view received image attachment: %#v", m.Input.ImageAttachments())
	}
}
