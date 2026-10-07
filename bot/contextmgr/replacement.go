package contextmgr

import (
	"fmt"
	"strings"

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

// summaryRequestReserve covers the summary system prompt, the requested
// 2000 completion tokens, and encoding slack between estimate and wire.
const summaryRequestReserve = 4096

func newReplacementCompactor(summarizer Summarizer, autoCompactPercent int) *replacementCompactor {
	return &replacementCompactor{
		summarizer:         summarizer,
		autoCompactPercent: normalizeAutoCompactPercent(autoCompactPercent),
		keepTurns:          3,
	}
}

// summarizerModelName resolves the model id for error messages; unknown
// clients report a placeholder instead of an empty string.
func summarizerModelName(model provider.LLM) string {
	if source, ok := model.(interface{ RequestMeta() types.RequestMeta }); ok {
		if name := source.RequestMeta().Model; name != "" {
			return name
		}
	}
	return "unknown"
}

func (s *replacementCompactor) shouldAutoCompact(budget, estimate int) bool {
	return estimate >= s.compactionThreshold(budget)
}

// archiveUnavailable marks an archive whose older messages were dropped without
// a usable summary. Reports must not present it as a normal archive.
const archiveUnavailable = "[Archive unavailable: summarizer output was malformed or too small; recent conversation was preserved.]"

// minFallbackArchiveRunes is the floor for accepting a reply that ignored the
// required <summary> format. Runes, not bytes: a byte floor is ~3x stricter for
// CJK text than for ASCII, which silently discarded usable short summaries.
const minFallbackArchiveRunes = 20

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

	// Pre-flight: refuse a summary request the summarizer cannot read.
	// A summary larger than the model's window is a guaranteed failure
	// (empty response or API rejection) and would otherwise retry forever.
	if s.modelWindow > 0 {
		input := token.EstimateTokens(toSummarize) + token.EstimateString(prevArchive)
		if input+summaryRequestReserve >= s.modelWindow {
			modelName := summarizerModelName(s.model)
			return "", nil, 0, fmt.Errorf(
				"待摘要上下文约 %d tokens（含旧摘要 %d）超过摘要模型 %q 的窗口 %d；"+
					"请切回大窗口模型执行 /compact 后再切换，或在配置中把 flash_model 指向大窗口轻量模型",
				input, token.EstimateString(prevArchive), modelName, s.modelWindow)
		}
	}

	rawSummary, err := summarizer(toSummarize, prevArchive)
	if err != nil {
		return "", nil, 0, fmt.Errorf("replacement compact: %w", err)
	}

	// A wrapped <summary> block is the contract, so its content is kept as-is
	// however short it is: only a reply that ignored the format is judged by
	// length, and then only to reject a refusal rather than a summary.
	archive := formatCompactSummary(rawSummary)
	if strings.TrimSpace(archive) == "" {
		// No usable <summary> content. A reply that never used the tags may
		// still be a summary; an empty wrapper is not one.
		archive = ""
		if !strings.Contains(rawSummary, "<summary>") {
			archive = strings.TrimSpace(rawSummary)
		}
		if len([]rune(archive)) < minFallbackArchiveRunes {
			archive = archiveUnavailable
			logger.Log("replacement_compact: summarizer produced no usable <summary>; %d older msgs dropped without a summary", len(toSummarize))
		}
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
		if isConversationUser(msgs[i]) {
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
		if isConversationUser(msgs[i]) {
			return i
		}
	}
	return -1
}

func isConversationUser(msg types.Message) bool {
	return msg.Role == "user" && msg.Source != types.MessageSourceRuntimeContext &&
		msg.Source != types.MessageSourceHint && msg.Source != types.MessageSourceRuntimeEvent
}

func excludeFromSummary(msg types.Message) bool {
	return msg.Source == types.MessageSourceRuntimeContext || msg.Source == types.MessageSourceHint
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
