package contextmgr

import (
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/provider/types"
)

type ContextReport struct {
	Budget              int
	SystemPrompt        int
	TodoText            int
	SkillList           int
	Memory              int
	Archive             int
	ToolDefTokens       int
	Messages            int
	HasArchive          bool
	Archived            int
	CompactCount        int
	CompactionThreshold int
	TrimCount           int
	ToolDefCount        int
	UserMessages        int
	SysInjections       int
	AssistantMsgs       int
	ToolResults         int
	CacheHitTokens      int
	CacheMissTokens     int
	CacheHitRatio       float64
	SubCount            int
	SubTokens           int
	SubCacheHit         int
	SubCacheMiss        int
	PrefixTurn          PrefixTurnStats
	// ArchiveUnavailable reports that the archive is the placeholder left when
	// a summary could not be produced, so it must not be shown as a normal one.
	ArchiveUnavailable bool
	// SavedTurn carries the previous process's last turn, and is only populated
	// while PrefixTurn is still empty (no request yet in this process).
	SavedTurn PrefixTurnStats
}

func (m *Manager) Report() ContextReport {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()

	r := ContextReport{}
	r.SystemPrompt = token.EstimateString(m.state.ctx.SystemPrompt)
	r.SkillList = token.EstimateString(m.state.ctx.Skills)
	r.Memory = token.EstimateString(m.state.ctx.Memory)
	r.Archive = token.EstimateString(m.state.ctx.Archive)

	for _, msg := range m.state.ctx.Messages {
		if msg.Content == clearedMarker {
			continue
		}
		switch msg.Role {
		case "user":
			if msg.Source == "system" || msg.Source == types.MessageSourceRuntimeContext ||
				msg.Source == types.MessageSourceHint || msg.Source == types.MessageSourceRuntimeEvent {
				r.SysInjections++
			} else {
				r.UserMessages++
			}
		case "assistant":
			r.AssistantMsgs++
		case "tool":
			r.ToolResults++
		}
	}
	r.Messages = token.EstimateModelTokens(m.state.ctx.Messages, m.state.reasoning)
	r.HasArchive = m.state.ctx.Archive != ""
	r.ArchiveUnavailable = m.state.ctx.Archive == archiveUnavailable
	r.Archived = m.state.trimCount
	r.CompactCount = m.state.compactCount
	r.CompactionThreshold = m.state.compressor.compactionThreshold(m.state.contextWindow)
	r.TrimCount = m.state.trimCount
	r.Budget = m.state.contextWindow
	r.CacheHitTokens, r.CacheMissTokens = m.state.tracker.CacheStats()
	r.CacheHitRatio = m.state.tracker.CacheHitRatio()
	sub := m.state.tracker.SubStats()
	r.SubCount = sub.Count
	r.SubTokens = sub.TotalTokens
	r.SubCacheHit = sub.CacheHitTokens
	r.SubCacheMiss = sub.CacheMissTokens
	r.PrefixTurn = m.state.prefix.TurnStats()
	if r.PrefixTurn.Requests == 0 {
		r.SavedTurn = cloneTurnStats(m.state.restoredTurn)
	}
	return r
}
