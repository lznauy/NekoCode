package contextmgr

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"nekocode/bot/decision"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/protocol"
)

type compactionSnapshot struct {
	history  []types.Message
	archive  string
	budget   int
	estimate int
	revision uint64
}

const (
	// minAssistantPruneRunes: only substantial assistant messages are worth
	// a judgment; trivial ones cost less to keep than to prune.
	minAssistantPruneRunes = 160
	// assistantCandidate marks assistant-message candidates to the pruner.
	assistantCandidate = "assistant"
)

// AutoCompactIfNeeded compacts only when the context has crossed the automatic
// threshold. The model call runs without holding the context state lock.
func (m *Manager) AutoCompactIfNeeded(ctx context.Context) (bool, error) {
	return m.compact(ctx, false)
}

// Summarize compacts the current history regardless of its token occupancy.
// It reports whether any messages were actually replaced.
func (m *Manager) Summarize(ctx context.Context) (bool, error) {
	return m.compact(ctx, true)
}

func (m *Manager) compact(ctx context.Context, force bool) (applied bool, resultErr error) {
	m.compactionMu.Lock()
	defer m.compactionMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	snap, compressor := m.compactionInput()
	if compressor == nil {
		return false, nil
	}
	if !force && !compressor.shouldAutoCompact(snap.budget, snap.estimate) {
		return false, nil
	}
	started := false
	start := time.Now()
	ev := protocol.CompactionEvent{ID: uuid.NewString(), Trigger: protocol.CompactionAuto, BeforeTokens: snap.estimate, BeforeMessages: len(snap.history)}
	if force {
		ev.Trigger = protocol.CompactionManual
	}
	m.usageMu.RLock()
	beforeCompaction := m.beforeCompaction
	m.usageMu.RUnlock()
	if beforeCompaction != nil {
		if err := beforeCompaction(ev.ID); err != nil {
			return false, fmt.Errorf("backup before compaction: %w", err)
		}
	}
	var streamed strings.Builder
	m.usageMu.RLock()
	observer := m.compactionObserver
	m.usageMu.RUnlock()
	emit := func() {
		if observer != nil {
			observer(ev)
		}
	}
	defer func() {
		if !started {
			return
		}
		ev.Delta = ""
		ev.ElapsedMs = time.Since(start).Milliseconds()
		ev.Status = protocol.CompactionCompleted
		if !applied {
			ev.Status = protocol.CompactionFailed
			ev.Summary = streamed.String()
			ev.AfterTokens, ev.AfterMessages = ev.BeforeTokens, ev.BeforeMessages
		}
		if resultErr != nil && applied {
			ev.Warning = resultErr.Error()
		} else if resultErr != nil {
			ev.Error = resultErr.Error()
		}
		emit()
		// Message indexes recorded against the pre-compaction history (for
		// example selected-skill ranges) are invalid after the replacement.
		// Notify while compactionMu is still held but state.mu is released.
		if applied {
			m.usageMu.RLock()
			after := m.afterCompaction
			m.usageMu.RUnlock()
			if after != nil {
				after()
			}
		}
	}()
	summarizer := compressor.summarizer
	if compressor.model != nil {
		summarizer = m.streamingSummarizer(ctx, compressor.model, func(delta string) {
			streamed.WriteString(delta)
			ev.Status, ev.Delta = protocol.CompactionDelta, delta
			emit()
		})
	}
	if summarizer != nil {
		// The wrapper marks the event started when the summarizer actually
		// runs. It is per-compaction state, so it is handed to summarize
		// instead of written back to compressor.summarizer: storing it there
		// would wrap the previous wrapper again on the next compaction and
		// replay its stale Started event.
		inner := summarizer
		summarizer = func(messages []types.Message, previous string) (string, error) {
			started = true
			ev.Status = protocol.CompactionStarted
			emit()
			return inner(messages, previous)
		}
	}

	// Optional relevance pruning (e.g. Jev): stale tool results in the
	// summarized prefix are replaced by placeholders before the summarizer
	// runs. Fail-open — pruner errors leave history untouched.
	snap.history = m.pruneStaleToolResults(ctx, snap.history, snap.budget, compressor)
	archive, recent, trimmed, err := compressor.summarize(snap.history, snap.archive, snap.budget, summarizer)
	if err != nil {
		if !force {
			return false, fmt.Errorf("auto compact failed: %w", err)
		}
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if trimmed == 0 {
		if !force && snap.estimate >= compressor.effectiveBudget(snap.budget) {
			return false, contextFullError(snap.estimate, snap.budget)
		}
		return false, nil
	}

	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if m.state.revision != snap.revision {
		return false, fmt.Errorf("context changed while compacting; summary was not applied")
	}
	m.state.ctx.Archive = archive
	m.state.ctx.Messages = recent
	m.restoreRuntimeContextLocked()
	m.state.revision++
	m.state.compactCount++
	m.state.trimCount += trimmed
	m.state.tracker.RecordPrompt(m.totalTokenEstimate())

	used := m.estimatedTokens()
	ev.Summary, ev.AfterTokens, ev.AfterMessages = archive, used, len(m.state.ctx.Messages)
	if !force && used >= compressor.effectiveBudget(snap.budget) {
		return true, contextFullError(used, snap.budget)
	}
	return true, nil
}

// SetCompactionObserver connects display events without adding them to history.
func (m *Manager) SetCompactionObserver(observer func(protocol.CompactionEvent)) {
	m.usageMu.Lock()
	m.compactionObserver = observer
	m.usageMu.Unlock()
}

// SetBeforeCompaction registers the durability barrier that must succeed
// before a summarizer is called.
func (m *Manager) SetBeforeCompaction(fn func(string) error) {
	m.usageMu.Lock()
	m.beforeCompaction = fn
	m.usageMu.Unlock()
}

// SetAfterCompaction registers a callback invoked after a compaction has
// replaced the active history, so index-based bookkeeping against the old
// message list can be reset before it misidentifies messages.
func (m *Manager) SetAfterCompaction(fn func()) {
	m.usageMu.Lock()
	m.afterCompaction = fn
	m.usageMu.Unlock()
}

func contextFullError(used, budget int) error {
	if budget <= 0 {
		budget = defaultBudget
	}
	return fmt.Errorf("context full: %d tokens used of %d budget (only %d remaining)",
		used, budget, budget-used)
}

// pruneStaleToolResults consults the optional ToolPruner for every tool
// result — and substantial plain assistant message — that compaction would
// summarize, replacing low-relevance content with a one-line placeholder.
// Tool-call records stay intact so provider message pairing remains valid.
// Recent messages (per the compactor's own retention boundary) are never
// pruned. Any unavailable judgment keeps the original content.
func (m *Manager) pruneStaleToolResults(ctx context.Context, history []types.Message, budget int, compressor *replacementCompactor) []types.Message {
	m.usageMu.RLock()
	pruner := m.toolPruner
	m.usageMu.RUnlock()
	if pruner == nil {
		return history
	}
	// summarize filters internal-context messages before computing its own
	// retention boundary; pruning must run on the same filtered view, or a
	// placeholder could replace a tool result that lands inside the recent
	// window after the filter shifts the indexes.
	history = retainLatestRuntimeContext(history)
	keepStart := compressor.recentStart(history, budget)
	if keepStart <= 0 {
		return history
	}

	// One reverse pass builds the tool-call name table; resolving per
	// candidate instead would rescan the whole history (O(n²)) while
	// holding compactionMu.
	toolNames := make(map[string]string, 16)
	for _, msg := range history {
		for _, call := range msg.ToolCalls {
			if call.ID != "" {
				toolNames[call.ID] = call.Function.Name
			}
		}
	}

	resultIdx := make([]int, 0, 16)
	var candidates []decision.Candidate
	for i := 0; i < keepStart; i++ {
		msg := history[i]
		var tool string
		switch {
		case msg.ToolCallID != "":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			tool = toolNames[msg.ToolCallID]
			if tool == "" {
				tool = "tool"
			}
		case msg.Role == "assistant" && len(msg.ToolCalls) == 0:
			// Plain assistant narration carries no pairing constraint, so it
			// can be placeholdered like a tool result when it lost relevance.
			if len([]rune(msg.Content)) < minAssistantPruneRunes {
				continue
			}
			tool = assistantCandidate
		default:
			continue
		}
		head := msg.Content
		if runes := []rune(head); len(runes) > 240 {
			head = string(runes[:240])
		}
		candidates = append(candidates, decision.Candidate{Tool: tool, Head: head})
		resultIdx = append(resultIdx, i)
	}
	if len(candidates) == 0 {
		return history
	}

	pruned, ok := pruner.PruneTools(ctx, goalFromHistory(history), candidates)
	if !ok || len(pruned) != len(candidates) {
		return history
	}
	out := append([]types.Message(nil), history...)
	prunedCount := 0
	for j, i := range resultIdx {
		if !pruned[j] {
			continue
		}
		label := "tool result"
		if candidates[j].Tool == assistantCandidate {
			label = "assistant message"
		}
		out[i].Content = fmt.Sprintf("[pruned stale %s: %s]", label, candidates[j].Head)
		prunedCount++
	}
	if prunedCount > 0 {
		logger.Log("compaction: relevance pruner replaced %d of %d old messages", prunedCount, len(candidates))
	}
	return out
}

// goalFromHistory builds the decision goal: the most recent real user turn
// plus the heads of the most recent tool results, so the pruner judges old
// content against what the task has actually been touching — not just the
// literal request text.
func goalFromHistory(history []types.Message) string {
	var goal string
	for i := len(history) - 1; i >= 0; i-- {
		if isConversationUser(history[i]) && strings.TrimSpace(history[i].Content) != "" {
			goal = truncateRunes(history[i].Content, 1000)
			break
		}
	}
	var heads []string
	for i := len(history) - 1; i >= 0 && len(heads) < 3; i-- {
		msg := history[i]
		if msg.ToolCallID == "" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		heads = append(heads, "- "+truncateRunes(msg.Content, 120))
	}
	if len(heads) > 0 {
		goal += "\n\nRecent tool results:\n" + strings.Join(heads, "\n")
	}
	return goal
}

func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if n <= 0 || len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}

func (m *Manager) compactionInput() (compactionSnapshot, *replacementCompactor) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	history := append([]types.Message(nil), m.state.ctx.Messages...)
	return compactionSnapshot{
		history: history, archive: m.state.ctx.Archive,
		budget: m.state.contextWindow, estimate: m.estimatedTokens(),
		revision: m.state.revision,
	}, cloneCompactor(m.state.compressor)
}

func cloneCompactor(src *replacementCompactor) *replacementCompactor {
	if src == nil {
		return nil
	}
	copy := *src
	return &copy
}
