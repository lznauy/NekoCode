package contextmgr

import (
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/llm/types"
)

func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := len(m.ctx.Messages)
	if m.ctx.CompactBoundary > 0 && m.ctx.CompactBoundary < n {
		return n - m.ctx.CompactBoundary
	}
	return n
}

func (m *Manager) Stats() (int, int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	visible := m.visibleMessages()
	return len(m.ctx.Messages),
		token.EstimateTokens(visible) + token.EstimateString(m.ctx.Archive),
		m.ctx.Archive != ""
}

func (m *Manager) visibleMessages() []types.Message {
	visible := m.ctx.Messages
	if m.ctx.CompactBoundary > 0 && m.ctx.Archive != "" && m.ctx.CompactBoundary < len(visible) {
		visible = visible[m.ctx.CompactBoundary:]
	}
	return visible
}
