package components

import (
	"nekocode/interaction/tui/components/processing"
	"nekocode/protocol"
	"strings"
)

func (m *Messages) UpdateCompaction(event protocol.CompactionEvent) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.UpdateCompaction(event) })
}

func (m *Messages) SettleCompactions() {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.SettleCompactions() })
}

// Suppress command feedback only when this run already has a manual result.
func (m *Messages) IsDuplicateCompactionResponse(content string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processingItem == nil || !m.processingItem.HasManualCompactionResult() {
		return false
	}
	return strings.HasPrefix(content, "Compacted:") || strings.HasPrefix(content, "Summary updated:") || strings.HasPrefix(content, "Compaction failed:")
}
