package message

import (
	"charm.land/lipgloss/v2"
	"nekocode/interaction/tui/styles"
	"nekocode/protocol"
	"strings"
	"testing"
)

func TestCompactionStreamAndSettle(t *testing.T) {
	sty := styles.DefaultStyles()
	e := protocol.CompactionEvent{ID: "summary-1", Status: "started", Trigger: "auto", BeforeMessages: 16}
	m := NewCompactionItem(&sty, e)
	e.Status, e.Delta = "delta", "压缩详情：保留已完成修改\x1b]52;c;evil\a"
	m.Update(e)
	view := m.Render(24)
	if strings.Contains(view, "evil") || lipgloss.Width(view) > 24 || !strings.Contains(view, "压缩详情") || !strings.Contains(view, "已完成修改") {
		t.Fatalf("unsafe/overflowing view: %q", view)
	}
	if strings.Contains(view, "Generating summary") {
		t.Fatal("placeholder should not render while streaming")
	}
	e.Status, e.Summary = "completed", "final summary"
	m.Update(e)
	if strings.Contains(m.Render(80), "final summary") {
		t.Fatal("completed entry must not show the summary")
	}
}

func TestCompactionFailedKeepsError(t *testing.T) {
	sty := styles.DefaultStyles()
	e := protocol.CompactionEvent{ID: "summary-2", Status: "delta", Trigger: "manual", BeforeMessages: 5, Summary: "partial summary"}
	m := NewCompactionItem(&sty, e)
	e.Status, e.Error = "failed", "Compaction interrupted; completion was not confirmed."
	m.Update(e)
	view := m.Render(80)
	if !strings.Contains(view, "Compaction incomplete") || !strings.Contains(view, "Compaction interrupted") || !strings.Contains(view, "partial summary") {
		t.Fatalf("failed entry lost error/partial summary: %q", view)
	}
}
