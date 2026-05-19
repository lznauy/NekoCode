package ctxmgr

import (
	"encoding/json"
	"nekocode/common"
	"os"
	"sync"

	"nekocode/bot/ctxmgr/compact"
	"nekocode/bot/ctxmgr/context"
	"nekocode/bot/ctxmgr/memory"
	"nekocode/bot/ctxmgr/token"
	"nekocode/llm"
)

type TokenStats struct {
	TokenBudget  int
	Tracker      *token.Tracker
	CompactCount int
	TrimCount    int
}

type Manager struct {
	mu          sync.RWMutex
	ctx         context.Content
	tok         TokenStats
	mem         *memory.File
	cm          *compact.Compactor
	mergeClient llm.LLM // for independent merge sessions
}

func New(systemPrompt string, mem *memory.File) *Manager {
	ctx := context.New(systemPrompt)
	if mem != nil {
		ctx.Memory = mem.Build()
	}
	m := &Manager{
		ctx: ctx,
		tok: TokenStats{Tracker: &token.Tracker{}},
		mem: mem,
	}
	m.cm = &compact.Compactor{
		Ctx:          &m.ctx,
		TokenBudget:  &m.tok.TokenBudget,
		Tracker:      m.tok.Tracker,
		CompactCount: &m.tok.CompactCount,
		TrimCount:    &m.tok.TrimCount,
		Cfg:          compact.DefaultConfig,
	}
	return m
}

// -- delegation --------------------------------------------------------

func (m *Manager) SetSummarizer(fn compact.Summarizer) { m.cm.SetSummarizer(fn) }
func (m *Manager) SetMergeClient(client llm.LLM)       { m.mergeClient = client }

// MergeArchive runs an independent LLM session to merge new summaries into the archive.
func (m *Manager) MergeArchive(oldSummary, newSummary string) string {
	if m.mergeClient == nil {
		return oldSummary + "\n\n" + newSummary
	}
	return compact.MergeSummaries(m.mergeClient, oldSummary, newSummary)
}
func (m *Manager) SetSystemPrompt(s string) { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.SystemPrompt = s }
func (m *Manager) SetSkillList(s string)    { m.ctx.Skills = s }
func (m *Manager) SetArchive(s string)      { m.ctx.Archive = s }
func (m *Manager) SetTodos(items []common.TodoItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.LoadTodos(items)
}
func (m *Manager) SetHints(s string) { m.mu.Lock(); defer m.mu.Unlock(); m.ctx.Hints = s }
func (m *Manager) AllTasksDone() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ctx.AllTasksDone()
}

func (m *Manager) RecordUsage(prompt, completion int) { m.tok.Tracker.RecordUsage(prompt, completion) }
func (m *Manager) RecordCache(hit, miss int)          { m.tok.Tracker.RecordCache(hit, miss) }
func (m *Manager) CacheStats() (hit, miss int)        { return m.tok.Tracker.CacheStats() }
func (m *Manager) CacheHitRatio() float64             { return m.tok.Tracker.CacheHitRatio() }
func (m *Manager) Memory() *memory.File               { return m.mem }
func (m *Manager) SetTokenBudget(budget int) {
	if budget > 0 {
		m.tok.TokenBudget = budget
	}
}

func (m *Manager) CompactCount() int    { m.mu.RLock(); defer m.mu.RUnlock(); return m.tok.CompactCount }
func (m *Manager) TrimCount() int       { m.mu.RLock(); defer m.mu.RUnlock(); return m.tok.TrimCount }
func (m *Manager) CompactBoundary() int { return m.ctx.CompactBoundary }

func (m *Manager) AutoCompactIfNeeded() (compact.Level, error) {
	return m.cm.AutoCompactIfNeeded()
}
func (m *Manager) MicroCompactIfNeeded() int { return m.cm.MicroCompactIfNeeded() }
func (m *Manager) ForceCompact()             { m.cm.ForceCompact() }
func (m *Manager) NeedsSummarization() bool  { return m.cm.NeedsSummarization() }
func (m *Manager) Summarize() error {
	prevArchive := m.ctx.Archive
	if err := m.cm.FullCompact(); err != nil {
		return err
	}
	// Merge new archive with previous using independent merge session.
	if prevArchive != "" && m.ctx.Archive != "" && m.mergeClient != nil {
		m.ctx.Archive = compact.MergeSummaries(m.mergeClient, prevArchive, m.ctx.Archive)
	}
	return nil
}

// -- query ------------------------------------------------------------

func (m *Manager) Len() int { m.mu.RLock(); defer m.mu.RUnlock(); return len(m.ctx.Messages) }
func (m *Manager) Stats() (int, int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.ctx.Messages), m.visibleEstimatedTokens(), m.ctx.Archive != ""
}
func (m *Manager) TokenUsage() (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalEstimatedTokens(), m.tok.TokenBudget
}

func (m *Manager) visibleEstimatedTokens() int {
	visible := m.ctx.Messages
	if m.ctx.CompactBoundary > 0 && m.ctx.Archive != "" && m.ctx.CompactBoundary < len(visible) {
		visible = visible[m.ctx.CompactBoundary:]
	}
	return token.EstimateTokens(visible) + token.EstimateString(m.ctx.Archive)
}

// totalEstimatedTokens estimates ALL messages, not just the visible window.
// Used for quota computation — must reflect real context pressure.
func (m *Manager) totalEstimatedTokens() int {
	return token.EstimateTokens(m.ctx.Messages) + token.EstimateString(m.ctx.Archive)
}

// -- Build ------------------------------------------------------------

func (m *Manager) Build(withTools bool) []llm.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.ctx.BuildLayer0()
	out = append(out, m.ctx.BuildLayer0Mem()...)
	out = append(out, m.ctx.BuildLayer05()...)
	kept := m.applyWindowAndBudget(out, withTools)
	out = append(out, m.filterValidMessages(kept)...)
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
		compact.Log("truncate_to: dropped %d messages (kept %d, was %d)", len(m.ctx.Messages)-n, n, len(m.ctx.Messages))
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
		compact.Log("remove_messages: dropped %d messages [%d:%d] (total now %d)", n, startIdx, endIdx, len(m.ctx.Messages)-n)
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
	m.ctx.Messages = make([]llm.Message, 0)
		compact.Log("fresh_start: clearing all %d messages", len(m.ctx.Messages))
	m.ctx.CompactBoundary = 0
	m.ctx.Todo = ""
	m.ctx.TodoItems = nil
	m.ctx.Hints = ""
	m.tok.Tracker = &token.Tracker{}
}

	// Snapshot captures the full context state for session persistence.
	func (m *Manager) Snapshot() (sysPrompt, skills, archive, memory string, compactBoundary int, messages []llm.Message, budget int) {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.ctx.SystemPrompt, m.ctx.Skills, m.ctx.Archive, m.ctx.Memory,
			m.ctx.CompactBoundary, m.ctx.Messages, m.tok.TokenBudget
	}

	// Restore reconstructs context state from a session snapshot.
	func (m *Manager) Restore(sysPrompt, skills, archive, memory string, compactBoundary int, messages []llm.Message, budget int) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.ctx.SystemPrompt = sysPrompt
		m.ctx.Skills = skills
		m.ctx.Archive = archive
		m.ctx.Memory = memory
		m.ctx.CompactBoundary = compactBoundary
		m.ctx.Messages = messages
		m.tok.TokenBudget = budget
		m.tok.Tracker = &token.Tracker{}
	}

// ExportToFile writes the exact messages sent to the LLM API to a JSON file.
func (m *Manager) ExportToFile(path string) error {
	data, err := json.MarshalIndent(m.Build(true), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// -- helpers ----------------------------------------------------------

func (m *Manager) applyWindowAndBudget(staticPrefix []llm.Message, withTools bool) []llm.Message {
	kept := m.ctx.Messages
	if m.ctx.CompactBoundary > 0 && m.ctx.Archive != "" && m.ctx.CompactBoundary < len(kept) {
		compact.Log("build_boundary: sliced off %d messages before boundary=%d (keeping %d)", m.ctx.CompactBoundary, m.ctx.CompactBoundary, len(kept)-m.ctx.CompactBoundary)
	}
	reserved := token.EstimateTokens(staticPrefix) + m.ctx.DynamicSuffixTokens()
	budget := m.tok.TokenBudget - reserved - token.Overhead(withTools)
	if token.EstimateTokens(kept) > budget {
		compact.Log("build_trim: estimated %d tokens exceed budget %d, trimming oldest messages (currently %d msgs)", token.EstimateTokens(kept), budget, len(kept))
	}
	for len(kept) > 2 {
		if token.EstimateTokens(kept) <= budget {
			break
		}
		drop := 2
		for drop < len(kept) && kept[drop].Role == "tool" {
			drop++
		}
		kept = kept[drop:]
	}
	for len(kept) > 0 && kept[0].Role == "tool" {
		kept = kept[1:]
	}
	if len(m.ctx.Messages) > 0 && len(kept) < len(m.ctx.Messages) {
		compact.Log("build_trim_result: %d -> %d messages after budget trimming", len(m.ctx.Messages), len(kept))
	}
	return kept
}

func (m *Manager) filterValidMessages(kept []llm.Message) []llm.Message {
	hasToolResult := make(map[string]bool)
	for _, msg := range kept {
		if msg.Role == "tool" && msg.ToolCallID != "" && msg.Content != compact.ClearedMarker {
			hasToolResult[msg.ToolCallID] = true
		}
	}
	validAssistantIdx := make(map[int]bool)
	for i, msg := range kept {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			allPresent := true
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" && !hasToolResult[tc.ID] {
					allPresent = false
					break
				}
			}
			if allPresent {
				validAssistantIdx[i] = true
			}
		}
	}
	validIDs := make(map[string]bool)
	filtered := make([]llm.Message, 0, len(kept))
	for i, msg := range kept {
		mm := msg
		if mm.Content == "" && mm.Role != "system" {
			mm.Content = "."
		}
		if mm.Role == "assistant" && len(mm.ToolCalls) > 0 {
			if !validAssistantIdx[i] {
				continue
			}
			for _, tc := range mm.ToolCalls {
				if tc.ID != "" {
					validIDs[tc.ID] = true
				}
			}
		}
		if mm.Role == "tool" {
			if mm.ToolCallID == "" || !validIDs[mm.ToolCallID] {
				continue
			}
		}
		filtered = append(filtered, mm)
	}
	if len(filtered) < len(kept) {
		compact.Log("filter_orphans: dropped %d orphaned messages (%d kept of %d)", len(kept)-len(filtered), len(filtered), len(kept))
	}
	return filtered
}
