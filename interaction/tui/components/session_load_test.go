package components

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strings"
	"testing"

	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/styles"
)

func BenchmarkRestore5500Messages(b *testing.B) {
	sty := styles.DefaultStyles()
	history := make([]message.ChatMessage, 5500)
	for i := range history {
		history[i] = message.ChatMessage{Role: "user", Content: fmt.Sprintf("historical message %d", i)}
	}
	for _, mode := range []string{"incremental", "paged"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m := NewMessages(100, 30, &sty)
				if mode == "paged" {
					m.ReplaceMessages(history)
				} else {
					for _, msg := range history {
						m.AddMessage(msg)
					}
					m.GotoBottom()
				}
			}
		})
	}
}

func TestReplaceMessagesPreservesHistoryAndClearsProcessing(t *testing.T) {
	sty := styles.DefaultStyles()
	m := NewMessages(100, 30, &sty)
	m.AddMessage(message.ChatMessage{Role: "user", Content: "old"})
	m.SetProcessing(true)
	m.Follow = false
	history := []message.ChatMessage{{Role: "user", Content: "question"}, {Role: "assistant", Content: "answer", Reasoning: "reason"}, {Role: "system", Content: "notice"}}
	m.ReplaceMessages(history)
	if m.Len() != len(history) || m.Processing || m.processingItem != nil || !m.Follow || !m.AtBottom() {
		t.Fatal("history/processing/scroll state incorrect")
	}
	rendered := m.Render()
	if strings.Contains(rendered, "old") || !strings.Contains(rendered, "question") || !strings.Contains(rendered, "answer") || !strings.Contains(rendered, "notice") {
		t.Fatalf("restored content: %s", rendered)
	}
	m.ReplaceMessages(nil)
	if m.Len() != 0 || !m.AtBottom() {
		t.Fatal("empty restore retained history")
	}
}

func TestRestoredHistoryPagesKeepOrderAndProcessingAnchor(t *testing.T) {
	sty := styles.DefaultStyles()
	m := NewMessages(100, 10, &sty)
	history := make([]message.ChatMessage, 230)
	for i := range history {
		history[i] = message.ChatMessage{Role: "user", Content: fmt.Sprintf("history-%03d-end", i)}
	}
	m.ReplaceMessages(history)
	if m.Len() != 101 || len(m.history) != 130 {
		t.Fatalf("initial loaded=%d remaining=%d", m.Len(), len(m.history))
	}
	if !strings.Contains(m.Items()[1].Render(100), "history-130-end") {
		t.Fatal("first page is not newest")
	}
	m.SetProcessing(true)
	active := m.processingItem
	oldFirst := m.Items()[1]
	m.ScrollToTop()
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if len(m.history) != 30 || m.Len() != 202 || m.Follow {
		t.Fatalf("first prepend: remaining=%d items=%d follow=%v", len(m.history), m.Len(), m.Follow)
	}
	if m.Items()[101] != oldFirst || m.Items()[m.Len()-1] != active {
		t.Fatal("prepend lost viewport items or processing item")
	}
	if !m.LoadOlderMessages() || m.LoadOlderMessages() {
		t.Fatal("older history was duplicated or lost")
	}
	if len(m.history) != 0 || m.Len() != 232 {
		t.Fatalf("complete loaded=%d remaining=%d", m.Len(), len(m.history))
	}
	for i := range history {
		if got := m.Items()[i+1].Render(100); !strings.Contains(got, fmt.Sprintf("history-%03d-end", i)) {
			t.Fatalf("history order at %d: %s", i, got)
		}
	}
	m.SetProcessing(false)
	m.GotoBottom()
	if !m.AtBottom() {
		t.Fatal("cannot return to bottom")
	}
	m.ReplaceMessages(nil)
	if m.LoadOlderMessages() || m.historyNotice != nil {
		t.Fatal("session switch retained previous history")
	}
}
