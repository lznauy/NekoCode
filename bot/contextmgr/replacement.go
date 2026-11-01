package contextmgr

import (
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
	"nekocode/logger"
)

// replacementCompactor implements replacement-history compaction.
// It summarizes the old visible prefix, then replaces active history with
// the archive plus recent messages in active history.
type replacementCompactor struct {
	model provider.LLM
	// modelWindow is the summarizer model's context window (0 = unknown,
	// skip the pre-flight check). It gates the summary request: sending
	// more tokens than the summarizer can read turns into a guaranteed
	// empty response or API rejection, so the request is refused up front
	// with an actionable error instead.
	modelWindow        int
	summarizer         Summarizer
	autoCompactPercent int
	keepTurns          int
}

func newReplacementCompactor(summarizer Summarizer, autoCompactPercent int) *replacementCompactor {
	return &replacementCompactor{
		summarizer:         summarizer,
		autoCompactPercent: normalizeAutoCompactPercent(autoCompactPercent),
		keepTurns:          3,
	}
}

func (s *replacementCompactor) shouldAutoCompact(budget, estimate int) bool {
	return estimate >= s.compactionThreshold(budget)
}

// archiveUnavailable recognizes legacy archives produced by the old lossy
// fallback. New compactions reject unusable summaries without dropping history.
const archiveUnavailable = "[Archive unavailable: summarizer output was malformed or too small; recent conversation was preserved.]"

// summarize runs one compaction. summarizer overrides the compactor's own for
// this call only (compact passes a per-invocation wrapper that emits the
// Started event); nil falls back to s.summarizer. Keeping the override out of
// the struct prevents a wrapper capturing one compaction's event state from
// being stored and re-wrapped by the next compaction.
func (s *replacementCompactor) summarize(history []types.Message, prevArchive string, budget int, summarizer Summarizer) (string, []types.Message, int, error) {
	originalLen := len(history)
	history = retainLatestRuntimeContext(history)
	if summarizer == nil {
		summarizer = s.summarizer
	}
	if summarizer == nil || len(history) <= 2 {
		return prevArchive, history, originalLen - len(history), nil
	}
	keepStart := s.recentStart(history, budget)
	if keepStart <= 0 {
		return prevArchive, history, originalLen - len(history), nil
	}

	toSummarize := withoutInternalContext(history[:keepStart])
	recent := append([]types.Message(nil), history[keepStart:]...)

	archive, err := s.summarizeBatches(toSummarize, prevArchive, summarizer)
	if err != nil {
		return "", nil, 0, err
	}

	logger.Log("replacement_compact: summarized %d msgs, kept %d recent msgs, archive_tokens=%d",
		len(toSummarize), len(recent), token.EstimateString(archive))
	return archive, recent, originalLen - len(recent), nil
}

func (s *replacementCompactor) recentStart(history []types.Message, budget int) int {
	start := userTurnBoundary(history, s.keepTurns)
	if start <= 0 {
		return start
	}

	maxRecentTokens := s.effectiveBudget(budget) / 3
	if maxRecentTokens < 2000 {
		maxRecentTokens = 2000
	}
	for token.EstimateTokens(history[start:]) > maxRecentTokens {
		next := nextUserBoundary(history, start+1)
		if next < 0 {
			break
		}
		start = next
	}
	return start
}

func userTurnBoundary(msgs []types.Message, turns int) int {
	if turns <= 0 {
		return len(msgs)
	}
	count := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].IsConversationUser() {
			count++
			if count >= turns {
				return i
			}
		}
	}
	return 0
}

func nextUserBoundary(msgs []types.Message, start int) int {
	for i := start; i < len(msgs); i++ {
		if msgs[i].IsConversationUser() {
			return i
		}
	}
	return -1
}

func excludeFromSummary(msg types.Message) bool {
	return msg.Source == types.MessageSourceRuntimeContext || msg.Source == types.MessageSourceHint || msg.Source == types.MessageSourceContextData
}

func withoutInternalContext(messages []types.Message) []types.Message {
	filtered := make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		if !excludeFromSummary(msg) {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func retainLatestRuntimeContext(messages []types.Message) []types.Message {
	latest := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Source == types.MessageSourceRuntimeContext {
			latest = i
			break
		}
	}
	filtered := make([]types.Message, 0, len(messages))
	for i, msg := range messages {
		if msg.Source == types.MessageSourceHint {
			continue
		}
		if msg.Source != types.MessageSourceRuntimeContext || i == latest {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func (s *replacementCompactor) effectiveBudget(budget int) int {
	if budget > 0 {
		return budget
	}
	return defaultBudget
}

func (s *replacementCompactor) compactionThreshold(budget int) int {
	return s.effectiveBudget(budget) * s.autoCompactPercent / 100
}
