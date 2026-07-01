package contextmgr

import (
	"nekocode/bot/contextmgr/token"
	"nekocode/common/debug"
)

// TruncateTo removes all messages from index n onward.
func (m *Manager) TruncateTo(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if n < len(m.ctx.Messages) {
		debug.Log("truncate_to: dropped %d messages (kept %d, was %d)", len(m.ctx.Messages)-n, n, len(m.ctx.Messages))
		m.ctx.Messages = m.ctx.Messages[:n]
	}
	if m.ctx.CompactBoundary > n {
		m.ctx.CompactBoundary = n
	}
}

func (m *Manager) RemoveMessages(startIdx, endIdx int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if startIdx < 0 || endIdx >= len(m.ctx.Messages) || startIdx > endIdx {
		return
	}
	n := endIdx - startIdx + 1
	m.ctx.Messages = append(m.ctx.Messages[:startIdx], m.ctx.Messages[endIdx+1:]...)
	debug.Log("remove_messages: dropped %d messages [%d:%d] (total now %d)", n, startIdx, endIdx, len(m.ctx.Messages))
	if m.ctx.CompactBoundary > startIdx {
		if m.ctx.CompactBoundary <= endIdx {
			m.ctx.CompactBoundary = startIdx
		} else {
			m.ctx.CompactBoundary -= n
		}
	}
}

func (m *Manager) FreshStart() {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.ctx.Messages)
	m.clearInternal()
	debug.Log("fresh_start: clearing all %d messages", n)
	m.ctx.Hints = ""
	m.Tracker = &token.Tracker{}
}
