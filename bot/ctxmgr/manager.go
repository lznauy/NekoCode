package ctxmgr

import (
	"context"
	"fmt"
	"sync"

	"nekocode/bot/ctxmgr/compact"
	ctxctx "nekocode/bot/ctxmgr/context"
	"nekocode/bot/ctxmgr/memory"
	"nekocode/bot/ctxmgr/token"
	"nekocode/bot/debug"
	"nekocode/common"
	"nekocode/llm/types"
)

type Manager struct {
	mu           sync.RWMutex
	ctx          ctxctx.Content
	ContextWindow int
	Tracker      *token.Tracker
	CompactCount int
	TrimCount    int
	mem          *memory.File
	CM           *compact.Compactor
	MergeClient  types.LLM // for independent merge archive sessions
}

type Config struct {
	SystemPrompt string
	Memory       *memory.File
	Summarizer   compact.Summarizer
	MergeClient  types.LLM
}

// NewSub creates a lightweight Manager for subagents.
// A Compactor is only created when mergeClient is non-nil (for archive merging).
func NewSub(systemPrompt string, contextWindow int, mergeClient types.LLM) *Manager {
	ctx := ctxctx.New(systemPrompt)
	m := &Manager{
		ctx:         ctx,
		Tracker:     &token.Tracker{},
		ContextWindow: contextWindow,
	}
	if mergeClient != nil {
		mergeCtx := context.Background()
		m.CM = &compact.Compactor{
			Ctx:           &m.ctx,
			ContextWindow:  &m.ContextWindow,
			Tracker:       m.Tracker,
			CompactCount:  &m.CompactCount,
			TrimCount:     &m.TrimCount,
			Summarizer:    MakeSummarizer(mergeCtx, mergeClient),
			CancelCtx:     mergeCtx,
			Cfg:           compact.DefaultConfig,
		}
	}
	return m
}

// MakeSummarizer creates a Summarizer func from an LLM client.
// The provided context is used for LLM calls, enabling cancellation.
func MakeSummarizer(ctx context.Context, client types.LLM) compact.Summarizer {
	return func(msgs []types.Message, prevSummary string) (string, error) {
		prompt := compact.BuildPrompt(msgs, prevSummary)
		resp, err := client.Chat(ctx, []types.Message{{Role: "user", Content: prompt}}, nil)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
			return resp.Choices[0].Message.Content, nil
		}
		return "", fmt.Errorf("no response from summarizer")
	}
}

func New(cfg Config) *Manager {
	ctx := ctxctx.New(cfg.SystemPrompt)
	if cfg.Memory != nil {
		ctx.Memory = cfg.Memory.Build()
	}
	m := &Manager{
		ctx:         ctx,
		Tracker:     &token.Tracker{},
		mem:         cfg.Memory,
		MergeClient: cfg.MergeClient,
	}
	m.CM = &compact.Compactor{
		Ctx:          &m.ctx,
		ContextWindow: &m.ContextWindow,
		Tracker:       m.Tracker,
		CompactCount:  &m.CompactCount,
		TrimCount:     &m.TrimCount,
		Summarizer:    cfg.Summarizer,
		CancelCtx:     context.Background(),
		Cfg:           compact.DefaultConfig,
	}
	return m
}
func (m *Manager) SetSystemPrompt(s string)            { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.SystemPrompt = s }
func (m *Manager) SetSkillList(s string)     { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.Skills = s }
func (m *Manager) SetHints(s string)         { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.Hints = s }
func (m *Manager) SetContextWindow(budget int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if budget > 0 { m.ContextWindow = budget }
}

func (m *Manager) SetTodos(items []common.TodoItem) { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.LoadTodos(items) }
func (m *Manager) AllTasksDone() bool               { m.mu.RLock(); defer m.mu.RUnlock(); return m.ctx.AllTasksDone() }
func (m *Manager) HasTasks() bool                   { m.mu.RLock(); defer m.mu.RUnlock(); return m.ctx.HasTasks() }
// RecordUsage, RecordCache, and ResetCache hold the read lock for the full
// call so FreshStart (which replaces m.Tracker under write lock) cannot race.
func (m *Manager) RecordUsage(prompt, completion int) { m.mu.RLock(); defer m.mu.RUnlock(); m.Tracker.RecordUsage(prompt, completion) }
func (m *Manager) RecordCache(hit, miss int)          { m.mu.RLock(); defer m.mu.RUnlock(); m.Tracker.RecordCache(hit, miss) }
func (m *Manager) ResetCache()                        { m.mu.RLock(); defer m.mu.RUnlock(); m.Tracker.ResetCache() }

func (m *Manager) AutoCompactIfNeeded() (compact.Level, error) {
	if m.CM != nil {
		return m.CM.AutoCompactIfNeeded()
	}
	return compact.LevelNormal, nil
}
func (m *Manager) NeedsSummarization() bool {
	if m.CM != nil {
		return m.CM.NeedsSummarization()
	}
	return false
}

func (m *Manager) Summarize() error {
	if m.CM == nil {
		return nil
	}
	// Phase 1: FullCompact mutates Messages and CompactBoundary — must be
	// serialized with other readers/writers.
	m.mu.Lock()
	prevArchive := m.ctx.Archive
	if err := m.CM.FullCompact(); err != nil {
		m.mu.Unlock()
		return err
	}
	newArchive := m.ctx.Archive
	m.mu.Unlock()

	// Phase 2: Merge new archive with previous using independent LLM session.
	// This can take seconds — run outside the lock so Build/Add/RecordUsage
	// are not blocked.
	if prevArchive != "" && newArchive != "" && m.MergeClient != nil {
		mergeCtx := m.CM.CancelCtx
		if mergeCtx == nil {
			mergeCtx = context.Background()
		}
		merged := compact.MergeSummaries(mergeCtx, m.MergeClient, prevArchive, newArchive)

		m.mu.Lock()
		m.ctx.Archive = merged
		m.mu.Unlock()
	}
	return nil
}

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
	visible := m.ctx.Messages
	if m.ctx.CompactBoundary > 0 && m.ctx.Archive != "" && m.ctx.CompactBoundary < len(visible) {
		visible = visible[m.ctx.CompactBoundary:]
	}
	return len(m.ctx.Messages),
		token.EstimateTokens(visible) + token.EstimateString(m.ctx.Archive),
		m.ctx.Archive != ""
}

func (m *Manager) TokenUsage() (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	visible := m.ctx.Messages
	if m.ctx.CompactBoundary > 0 && m.ctx.Archive != "" && m.ctx.CompactBoundary < len(visible) {
		visible = visible[m.ctx.CompactBoundary:]
	}
	return token.EstimateTokens(visible) + token.EstimateString(m.ctx.Archive), m.ContextWindow
}

// -- Build ------------------------------------------------------------

func (m *Manager) Build(withTools bool) []types.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.ctx.BuildLayer0()
	out = append(out, m.ctx.BuildLayer0Mem()...)
	out = append(out, m.ctx.BuildLayer05()...)
	out = append(out, m.filterValidMessages(m.ctx.Messages)...)
	out = append(out, m.ctx.BuildLayer2()...)
	return out
}

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
}


