package contextmgr

import (
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/provider/types"
)

type ManagerSnapshot struct {
	SystemPrompt string
	Skills       string
	Archive      string
	Memory       string
	Hints        string
	Messages     []types.Message
	Transcript   []types.Message
	Budget       int
	Tracker      token.State
	// PrefixBaseline identifies the last request's cache-relevant head so a
	// resumed session reports why the first call missed instead of a bare
	// cold-start.
	PrefixBaseline PrefixBaseline
	// PrefixTurn is the last turn's cache summary. It survives a reload so
	// /context can still show the previous turn before this process runs one.
	// Nil when no turn has been accounted.
	PrefixTurn *PrefixTurnStats
	// CompactCount and TrimCount are cumulative diagnostics for the archive
	// produced by compaction, so they round-trip with the rest of the session.
	CompactCount int
	TrimCount    int
}

func (m *Manager) Snapshot() ManagerSnapshot {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	msgs := make([]types.Message, len(m.state.ctx.Messages))
	copy(msgs, m.state.ctx.Messages)
	transcript := append([]types.Message(nil), m.state.transcript...)
	return ManagerSnapshot{
		SystemPrompt:   m.state.ctx.SystemPrompt,
		Skills:         m.state.ctx.Skills,
		Archive:        m.state.ctx.Archive,
		Memory:         m.state.ctx.Memory,
		Hints:          m.state.ctx.Hints,
		Messages:       msgs,
		Transcript:     transcript,
		Budget:         m.state.contextWindow,
		Tracker:        m.state.tracker.Snapshot(),
		PrefixBaseline: m.state.prefix.Baseline(),
		PrefixTurn:     m.turnToPersistLocked(),
		CompactCount:   m.state.compactCount,
		TrimCount:      m.state.trimCount,
	}
}

// turnToPersistLocked returns this process's turn, or the restored one when no
// request has been made yet. Falling back keeps the saved turn from being
// blanked by a save that happens before the first post-reload turn.
func (m *Manager) turnToPersistLocked() *PrefixTurnStats {
	turn := m.state.prefix.TurnStats()
	if turn.Requests == 0 {
		turn = cloneTurnStats(m.state.restoredTurn)
	}
	if turn.IsZero() {
		return nil
	}
	return &turn
}

func (m *Manager) Restore(snap ManagerSnapshot) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.ctx.SystemPrompt = snap.SystemPrompt
	m.state.ctx.Skills = snap.Skills
	m.state.ctx.Archive = snap.Archive
	m.state.ctx.Memory = snap.Memory
	m.state.ctx.Hints = snap.Hints
	m.state.runtimePolicy = ""
	m.state.ctx.Messages = append([]types.Message(nil), snap.Messages...)
	if snap.Transcript != nil {
		m.state.transcript = append([]types.Message(nil), snap.Transcript...)
	} else {
		m.state.transcript = append([]types.Message(nil), snap.Messages...)
	}
	m.restoreRuntimeContextLocked()
	if m.state.tracker == nil {
		m.state.tracker = &token.Tracker{}
	}
	// Model limits and provider-calibrated prompt usage belong to the active
	// runtime, not to persisted conversation content: a session may be resumed
	// with a different model, so retaining either can delay compaction past the
	// new model's actual context window. Cumulative cache hit/miss counts are
	// session-scoped diagnostics that only feed /context, so they are restored
	// to keep the "Session" figure continuous across reloads. Sub-agent totals
	// are session history and round-trip for the same reason.
	m.state.tracker.Restore(token.State{
		CacheHitTokens:  snap.Tracker.CacheHitTokens,
		CacheMissTokens: snap.Tracker.CacheMissTokens,
		Sub:             snap.Tracker.Sub,
	})
	m.state.prefix.Reset()
	// Seed the last request's head so the first post-resume call is attributed
	// to a real change (or to the provider) instead of reporting cold-start.
	m.state.prefix.RestoreBaseline(snap.PrefixBaseline)
	// Keep the previous turn's cache summary available until this process runs
	// a turn of its own.
	if snap.PrefixTurn != nil {
		m.state.restoredTurn = cloneTurnStats(*snap.PrefixTurn)
	} else {
		m.state.restoredTurn = PrefixTurnStats{}
	}
	// Compaction counters describe the restored archive, so they must come back
	// with it: zeroing them would report an archive that no compaction produced.
	m.state.compactCount = snap.CompactCount
	m.state.trimCount = snap.TrimCount
	m.state.revision++
}
