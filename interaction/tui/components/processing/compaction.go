package processing

import (
	"strings"

	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/styles"
	"nekocode/protocol"

	"charm.land/lipgloss/v2"
)

// CompactionHistory settles finished compactions into the transcript as flat
// tool-call style entries. The processing card frame itself is transient and
// must not survive the run.
type CompactionHistory struct {
	sty     *styles.Styles
	entries []*message.CompactionItem
}

func (p *ProcessingItem) CompactionHistory() *CompactionHistory {
	if len(p.compactions) == 0 {
		return nil
	}
	return &CompactionHistory{sty: p.sty, entries: p.compactions}
}

// Render draws each compaction as a tool-call style entry, without the
// transient processing card border.
func (h *CompactionHistory) Render(width int) string {
	contentW := styles.MessageWidth(width) - 4
	entries := make([]string, 0, len(h.entries))
	for _, detail := range h.entries {
		entries = append(entries, detail.Render(contentW))
	}
	return strings.Join(entries, "\n\n")
}

func (h *CompactionHistory) Height(width int) int {
	return strings.Count(h.Render(width), "\n") + 1
}

func (p *ProcessingItem) UpdateCompaction(event protocol.CompactionEvent) {
	defer p.invalidateLight()
	for _, detail := range p.compactions {
		if detail.Event.ID == event.ID {
			detail.Update(event)
			return
		}
	}
	p.compactions = append(p.compactions, message.NewCompactionItem(p.sty, event))
}

func (p *ProcessingItem) HasManualCompactionResult() bool {
	for _, detail := range p.compactions {
		if detail.Event.Trigger == protocol.CompactionManual && !detail.Running() {
			return true
		}
	}
	return false
}

func (p *ProcessingItem) SettleCompactions() {
	for _, detail := range p.compactions {
		if detail.Running() {
			event := detail.Event
			event.Status, event.Error = protocol.CompactionFailed, "Compaction interrupted; completion was not confirmed."
			detail.Update(event)
		}
	}
	p.invalidateLight()
}

func (p *ProcessingItem) renderCompactions(width int) string {
	if len(p.compactions) == 0 {
		return ""
	}
	var sb strings.Builder
	sep := p.sty.Primary.Render("── compaction " + strings.Repeat("─", max(0, width-lipgloss.Width("── compaction "))))
	sb.WriteString(sep)
	sb.WriteString("\n")
	entries := make([]string, 0, len(p.compactions))
	for _, detail := range p.compactions {
		entries = append(entries, detail.Render(width))
	}
	sb.WriteString(strings.Join(entries, "\n\n"))
	// Keep one blank line between the stream and the spinner header below.
	sb.WriteString("\n")
	return sb.String()
}
