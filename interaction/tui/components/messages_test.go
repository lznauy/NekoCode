package components

import (
	"strings"
	"testing"

	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/components/processing"
	"nekocode/interaction/tui/styles"
)

// Regression: a message arriving while the processing item is active must be
// inserted before it — the processing item has to stay the last list item,
// otherwise it renders above chat messages and tick-driven updates (which
// invalidate the last item) stop reaching it.
func TestAddMessageDuringProcessingKeepsProcessingItemLast(t *testing.T) {
	sty := styles.DefaultStyles()
	m := NewMessages(80, 8, &sty)
	m.AddMessage(message.ChatMessage{Role: "user", Content: "first"})
	m.SetProcessing(true)

	m.AddMessage(message.ChatMessage{Role: "user", Content: "second"})
	m.AddMessage(message.ChatMessage{Role: "system", Content: "note"})

	items := m.Items()
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4", len(items))
	}
	if _, ok := items[len(items)-1].(*processing.ProcessingItem); !ok {
		t.Fatalf("last item is %T, want *processing.ProcessingItem pinned at the end", items[len(items)-1])
	}
}

func TestSetProcessingOffKeepsBottomWhenFollowing(t *testing.T) {
	sty := styles.DefaultStyles()
	m := NewMessages(80, 8, &sty)
	m.AddMessage(message.ChatMessage{
		Role:    "assistant",
		Content: strings.Repeat("command output line\n", 80),
	})
	m.GotoBottom()
	if !m.AtBottom() {
		t.Fatal("expected initial bottom")
	}

	m.SetProcessing(true)
	if !m.AtBottom() {
		t.Fatal("expected processing start to remain at bottom")
	}

	m.SetProcessing(false)
	if !m.AtBottom() {
		t.Fatal("expected processing removal to remain at bottom")
	}
}

func TestMoveProcessingFromPreservesRenderingSuspension(t *testing.T) {
	for _, live := range []bool{false, true} {
		sty := styles.DefaultStyles()
		previous := NewMessages(80, 8, &sty)
		if live {
			previous.SetProcessing(true)
			previous.ProcessStreamText("retained output")
		}
		previous.SetRenderingSuspended(true)
		next := NewMessages(80, 8, &sty)
		probe := &countingListItem{height: 2}
		next.AppendItems(probe)
		next.MoveProcessingFrom(previous)
		if !live {
			next.SetProcessing(true)
			next.ProcessStreamText("retained output")
		}
		if probe.renders != 0 {
			t.Fatalf("live=%v: migrating hidden messages rendered the history %d times", live, probe.renders)
		}
		next.SetRenderingSuspended(false)
		if view := next.Render(); !strings.Contains(view, "retained output") || !next.AtBottom() {
			t.Fatalf("live=%v: resuming lost output or follow position: %s", live, view)
		}
	}
}
