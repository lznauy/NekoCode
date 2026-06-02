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
// No Compactor — subagents don't do LLM-based compaction.
func NewSub(systemPrompt string, contextWindow int, mergeClient types.LLM) *Manager {
	ctx := ctxctx.New(systemPrompt)
	m := &Manager{
		ctx:         ctx,
		Tracker:     &token.Tracker{},
		ContextWindow: contextWindow,
	}
	if mergeClient != nil {
		m.CM = &compact.Compactor{
			Ctx:           &m.ctx,
			ContextWindow:  &m.ContextWindow,
			Tracker:       m.Tracker,
			CompactCount:  &m.CompactCount,
			TrimCount:     &m.TrimCount,
			Summarizer:    makeSummarizer(mergeClient),
			Cfg:           compact.DefaultConfig,
		}
	}
	return m
}

// makeSummarizer creates a Summarizer func from an LLM client.
func makeSummarizer(client types.LLM) compact.Summarizer {
	return func(msgs []types.Message, prevSummary string) (string, error) {
		prompt := compact.BuildPrompt(msgs, prevSummary)
		resp, err := client.Chat(context.Background(), []types.Message{{Role: "user", Content: prompt}}, nil)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) > 0 {
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
		Cfg:           compact.DefaultConfig,
	}
	return m
}
func (m *Manager) SetSystemPrompt(s string)            { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.SystemPrompt = s }
func (m *Manager) SetSkillList(s string)     { m.ctx.Skills = s }
func (m *Manager) SetHints(s string)         { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.Hints = s }
func (m *Manager) SetContextWindow(budget int) { if budget > 0 { m.ContextWindow = budget } }

func (m *Manager) SetTodos(items []common.TodoItem) { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.LoadTodos(items) }
func (m *Manager) AllTasksDone() bool               { m.mu.RLock(); defer m.mu.RUnlock(); return m.ctx.AllTasksDone() }
func (m *Manager) RecordUsage(prompt, completion int) { m.Tracker.RecordUsage(prompt, completion) }
func (m *Manager) RecordCache(hit, miss int)          { m.Tracker.RecordCache(hit, miss) }

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
	prevArchive := m.ctx.Archive
	if err := m.CM.FullCompact(); err != nil {
		return err
	}
	// Merge new archive with previous using independent merge session.
	if prevArchive != "" && m.ctx.Archive != "" && m.MergeClient != nil {
		m.ctx.Archive = compact.MergeSummaries(m.MergeClient, prevArchive, m.ctx.Archive)
	}
	return nil
}

func (m *Manager) Len() int { return len(m.ctx.Messages) }

func (m *Manager) Stats() (int, int, bool) {
	visible := m.ctx.Messages
	if m.ctx.CompactBoundary > 0 && m.ctx.Archive != "" && m.ctx.CompactBoundary < len(visible) {
		visible = visible[m.ctx.CompactBoundary:]
	}
	return len(m.ctx.Messages),
		token.EstimateTokens(visible) + token.EstimateString(m.ctx.Archive),
		m.ctx.Archive != ""
}

func (m *Manager) TokenUsage() (int, int) {
	return token.EstimateTokens(m.ctx.Messages) + token.EstimateString(m.ctx.Archive), m.ContextWindow
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
		debug.Log("remove_messages: dropped %d messages [%d:%d] (total now %d)", n, startIdx, endIdx, len(m.ctx.Messages)-n)
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
	m.ctx.Messages = make([]types.Message, 0)
	debug.Log("fresh_start: clearing all %d messages", n)
	m.ctx.CompactBoundary = 0
	m.ctx.Todo = ""
	m.ctx.TodoItems = nil
	m.ctx.Hints = ""
	m.Tracker = &token.Tracker{}
}

func (m *Manager) Snapshot() (sysPrompt, skills, archive, memory string, compactBoundary int, messages []types.Message, budget int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ctx.SystemPrompt, m.ctx.Skills, m.ctx.Archive, m.ctx.Memory,
		m.ctx.CompactBoundary, m.ctx.Messages, m.ContextWindow
}

func (m *Manager) Restore(sysPrompt, skills, archive, memory string, compactBoundary int, messages []types.Message, budget int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.SystemPrompt = sysPrompt
	m.ctx.Skills = skills
	m.ctx.Archive = archive
	m.ctx.Memory = memory
	m.ctx.CompactBoundary = compactBoundary
	m.ctx.Messages = messages
	m.ContextWindow = budget
	m.Tracker = &token.Tracker{}
}


