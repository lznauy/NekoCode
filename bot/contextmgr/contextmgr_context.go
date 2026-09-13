package contextmgr

import (
	"context"
	"fmt"
	"strings"

	"nekocode/bot/calllog"
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/protocol"
)

// ModelRequest describes one complete provider request projection.
type ModelRequest struct {
	Tools       []types.ToolDef
	PolicyHints string
}

// Build assembles the persisted context without mutating it:
//
//	Layer 0  system prompt + skill list (immutable within a session)
//	Layer 1  long-term memory
//	Layer 2  compaction archive
//	Layer 3  message history (append-only)
//
// Dynamic runtime state is represented by tagged user messages already
// appended to Layer 3 by BuildRequest.
func (m *Manager) Build() []types.Message {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	return m.buildLocked(false)
}

// BuildRequest assembles one model request and records its cache-relevant
// shape in the same state snapshot. Model callers should use this instead of
// pairing Build and prefix observation manually.
func (m *Manager) BuildRequest(request ModelRequest) []types.Message {
	runtimePrompt := m.renderRuntimeEnvironment()
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.appendRuntimeContextLocked(runtimePrompt)
	m.appendHintsLocked(m.state.ctx.Hints, request.PolicyHints)
	out := m.buildLocked(true)
	system := m.state.ctx.BuildLayer0()
	system = append(system, m.state.ctx.BuildLayer1()...)
	history := m.state.ctx.BuildLayer2()
	history = append(history, m.visibleHistory()...)
	out = types.ProjectReasoning(out, m.state.reasoning)
	history = types.ProjectReasoning(history, m.state.reasoning)
	m.state.prefix.Observe(buildPrefixShape(system, history, request.Tools))
	return out
}

func (m *Manager) buildLocked(preserveSource bool) []types.Message {
	out := m.state.ctx.BuildLayer0()
	out = append(out, m.state.ctx.BuildLayer1()...)
	out = append(out, m.state.ctx.BuildLayer2()...)
	out = append(out, m.visibleHistory()...)
	if !preserveSource {
		for i := range out {
			out[i].Source = ""
		}
	}
	return out
}

// appendRuntimeContextLocked appends a complete dynamic-state snapshot only
// when its provider-visible bytes changed. Explicit per-field states supersede
// old values so stale todos or runtime policies do not remain active.
func (m *Manager) appendRuntimeContextLocked(environment string) {
	projection := &m.state.runtimeProjection
	todos := m.state.ctx.TodoText()
	if !hasRuntimeContextState(environment, todos, m.state.runtimePolicy) && !projection.seen {
		return
	}
	current := renderRuntimeContext(environment, todos, m.state.runtimePolicy)
	if !projection.changed(current) {
		return
	}
	m.state.ctx.Messages = append(m.state.ctx.Messages, types.Message{
		Role: "user", Content: current, Source: types.MessageSourceRuntimeContext,
	})
	m.state.tracker.AddNew(len("user") + len(current))
	m.state.revision++
}

// appendHintsLocked appends only the active hint payload. Clearing a hint is
// controller lifecycle state, not a new model instruction, so it only rearms
// deduplication and does not append an empty/superseding runtime snapshot.
func (m *Manager) appendHintsLocked(hints ...string) {
	var active []string
	for _, hint := range hints {
		if hint = strings.TrimSpace(hint); hint != "" {
			active = append(active, hint)
		}
	}
	current := strings.Join(active, "\n")
	if current == "" {
		m.state.hintProjection.reset()
		return
	}
	if !m.state.hintProjection.changed(current) {
		return
	}
	m.state.ctx.Messages = append(m.state.ctx.Messages, types.Message{
		Role: "user", Content: current, Source: types.MessageSourceHint,
	})
	m.state.tracker.AddNew(len("user") + len(current))
	m.state.revision++
}

func hasRuntimeContextState(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func renderRuntimeContext(environment, todo, runtimePolicy string) string {
	blocks := []string{
		"<snapshot_semantics>This complete snapshot supersedes all earlier runtime_context messages.</snapshot_semantics>",
		renderRuntimeSection("environment_state", environment, "unavailable"),
		renderRuntimeSection("todo_state", formatTodo(todo), "empty"),
		renderRuntimeSection("runtime_policy_state", runtimePolicy, "inactive"),
	}
	return "<runtime_context mode=\"replace\">\n" + strings.Join(blocks, "\n") + "\n</runtime_context>"
}

func renderRuntimeSection(name, value, emptyState string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Sprintf("<%s state=\"%s\"></%s>", name, emptyState, name)
	}
	return fmt.Sprintf("<%s>\n%s\n</%s>", name, value, name)
}

// visibleHistory returns the post-compaction history with
// orphaned tool calls/results filtered out.
func (m *Manager) visibleHistory() []types.Message {
	return m.filterValidMessages(m.state.ctx.Messages)
}

// renderRuntimeEnvironment renders the current environment block via the
// registered provider ("" when none is set).
func (m *Manager) renderRuntimeEnvironment() string {
	provider := m.runtimePrompt
	if provider == nil {
		return ""
	}
	return provider()
}

func (m *Manager) filterValidMessages(kept []types.Message) []types.Message {
	hasResult := map[string]bool{}
	for _, msg := range kept {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			hasResult[msg.ToolCallID] = true
		}
	}
	validAsst := map[int]bool{}
	for i, msg := range kept {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			ok := true
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" && !hasResult[tc.ID] {
					ok = false
					break
				}
			}
			if ok {
				validAsst[i] = true
			}
		}
	}
	validIDs := map[string]bool{}
	filtered := make([]types.Message, 0, len(kept))
	for i, msg := range kept {
		if msg.Content == "" && msg.Role != "system" {
			msg.Content = "."
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			if !validAsst[i] {
				continue
			}
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					validIDs[tc.ID] = true
				}
			}
		}
		if msg.Role == "tool" && (msg.ToolCallID == "" || !validIDs[msg.ToolCallID]) {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func (m *Manager) SetSystemPrompt(systemPrompt string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.ctx.SystemPrompt = systemPrompt
}

func (m *Manager) SetSkillList(skillList string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.ctx.Skills = skillList
}

func (m *Manager) SetHints(hints string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.ctx.Hints = hints
}

// SetRuntimePolicy updates controller-owned policy for subsequent requests.
// The policy is emitted as part of a tagged user message, never by rewriting
// the stable system prompt.
func (m *Manager) SetRuntimePolicy(policy string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.runtimePolicy = policy
}

// ModelContext contains the context settings that change together when the
// active model changes.
type ModelContext struct {
	Window             int
	AutoCompactPercent int
	CompactionModel    provider.LLM
	Reasoning          types.ReasoningSettings
}

// ConfigureModel updates model-dependent context settings atomically.
func (m *Manager) ConfigureModel(cfg ModelContext) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if cfg.Window > 0 {
		m.state.contextWindow = cfg.Window
	}
	m.state.tracker.ResetModel()
	m.state.reasoning = cfg.Reasoning
	m.state.prefix.Reset()
	if m.state.compressor != nil {
		m.state.compressor.autoCompactPercent = normalizeAutoCompactPercent(cfg.AutoCompactPercent)
		var summarizer Summarizer
		if cfg.CompactionModel != nil {
			summarizer = m.makeSummarizer(context.Background(), cfg.CompactionModel)
		}
		m.state.compressor.summarizer = summarizer
		m.state.compressor.model = cfg.CompactionModel
	}
}

func (m *Manager) resetRuntimeContextLocked() {
	m.state.runtimeProjection.reset()
	m.state.hintProjection.reset()
}

func (m *Manager) restoreRuntimeContextLocked() {
	m.resetRuntimeContextLocked()
	for i := len(m.state.ctx.Messages) - 1; i >= 0; i-- {
		msg := m.state.ctx.Messages[i]
		if msg.Source == types.MessageSourceRuntimeContext {
			m.state.runtimeProjection.changed(msg.Content)
			return
		}
	}
}

func (m *Manager) SetTodos(items []protocol.TodoItem) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.ctx.LoadTodos(items)
}

func (m *Manager) Len() int {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	return len(m.state.ctx.Messages)
}

type Status struct {
	Messages     int
	Tokens       int
	Budget       int
	HasArchive   bool
	CacheHit     int
	CacheMiss    int
	HasTasks     bool
	TasksDone    bool
	CompactCount int
}

// Status returns one coherent snapshot of context occupancy and session usage.
func (m *Manager) Status() Status {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	hit, miss := m.state.tracker.CacheStats()
	return Status{
		Messages: len(m.state.ctx.Messages), Tokens: m.estimatedTokens(),
		Budget: m.state.contextWindow, HasArchive: m.state.ctx.Archive != "",
		CacheHit: hit, CacheMiss: miss,
		HasTasks: m.state.ctx.HasTasks(), TasksDone: m.state.ctx.AllTasksDone(),
		CompactCount: m.state.compactCount,
	}
}

// BeginModelTurn starts cache diagnostics for one user conversation. It keeps
// the prior request shape so the next request can still detect prefix changes.
func (m *Manager) BeginModelTurn() {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.prefix.BeginTurn()
}

// RecordModelUsage applies one complete provider usage event to token and
// prefix diagnostics. The stream layer emits this once per LLM request.
func (m *Manager) RecordModelUsage(usage types.StreamUsage) {
	if !usage.HasTokens() {
		return
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.tracker.RecordPrompt(usage.PromptTokens)
	if usage.CacheUsageReported {
		m.state.tracker.RecordCache(usage.CacheHitTokens, usage.CacheMissTokens)
		diagnosis := m.state.prefix.RecordCache(usage.CacheHitTokens, usage.CacheMissTokens)
		if usage.CacheMissTokens > 0 {
			logger.Log("prefix cache miss: tokens=%d changed=%v", usage.CacheMissTokens, diagnosis.Parts)
		}
	}
}

// PrefixDiagnostics exposes the current request's prefix fingerprint and
// change classification for the per-call evidence log.
func (m *Manager) PrefixDiagnostics() calllog.PrefixDiag {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	return m.state.prefix.Diagnostics()
}

func (m *Manager) RecordSubagent(tokens, hit, miss int) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.tracker.RecordSubagent(tokens, hit, miss)
}

func (m *Manager) totalTokenEstimate() int {
	return token.EstimateString(m.state.ctx.SystemPrompt) +
		token.EstimateString(m.state.ctx.Skills) +
		token.EstimateString(m.state.ctx.Memory) +
		token.EstimateString(m.state.ctx.Archive) +
		token.EstimateModelTokens(m.state.ctx.Messages, m.state.reasoning)
}

func (m *Manager) estimatedTokens() int {
	estimated := m.totalTokenEstimate()
	if calibrated := m.state.tracker.PromptEstimate(); calibrated > estimated {
		return calibrated
	}
	return estimated
}
