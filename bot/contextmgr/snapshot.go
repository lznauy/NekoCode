package contextmgr

import (
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/llm/types"
)

// ManagerSnapshot captures the full context manager state for session persistence.
type ManagerSnapshot struct {
	SystemPrompt    string
	Skills          string
	Archive         string
	Memory          string
	Hints           string
	CompactBoundary int
	Messages        []types.Message
	Budget          int
	Tracker         token.State
}

func (m *Manager) Snapshot() ManagerSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ManagerSnapshot{
		SystemPrompt:    m.ctx.SystemPrompt,
		Skills:          m.ctx.Skills,
		Archive:         m.ctx.Archive,
		Memory:          m.ctx.Memory,
		Hints:           m.ctx.Hints,
		CompactBoundary: m.ctx.CompactBoundary,
		Messages:        m.ctx.Messages,
		Budget:          m.ContextWindow,
		Tracker:         m.Tracker.Snapshot(),
	}
}

func (m *Manager) Restore(s ManagerSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.SystemPrompt = s.SystemPrompt
	m.ctx.Skills = s.Skills
	m.ctx.Archive = s.Archive
	m.ctx.Memory = s.Memory
	m.ctx.Hints = s.Hints
	m.ctx.CompactBoundary = s.CompactBoundary
	m.ctx.Messages = s.Messages
	m.ContextWindow = s.Budget
	m.Tracker = &token.Tracker{}
	m.Tracker.Restore(s.Tracker)
}
