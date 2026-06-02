package compact

import (
	"nekocode/bot/debug"
	"fmt"
	"strings"

	"nekocode/bot/ctxmgr/token"
	"nekocode/llm/types"
)

// Layer 5: Auto-Compaction.
// Head-Tail-Summary Reconstruction using a Server-side Forking Call.
// This is the last resort — all lighter layers have been tried.
//
// Head: most recent 3 turns (preserved intact — LLM's working context).
// Tail: everything between compactBoundary and Head, compressed into Archive.
// The result is a reconstructed context: Layer 0 + Layer 0.5(Archive) + Head.
//
// Uses a forking call pattern: dedicated LLM request with tools=nil,
// thinking=disabled, and a strict no-tools preamble. This call is isolated
// from the main agent's conversation flow.

// FullCompact performs Head-Tail-Summary Reconstruction.
func (m *Compactor) FullCompact() error {
	if m.Summarizer == nil {
		return nil
	}

	// Head: preserve at least 3 recent turns.
	const preserveTurns = 3
	headKeep := m.countMessagesForLastNTurns(preserveTurns)
	keep := 10
	if headKeep > keep {
		keep = headKeep
	}
	if keep < 2 {
		keep = 2
	}

	msgs := m.Ctx.Messages
	if len(msgs) <= keep {
		return nil
	}

	split := len(msgs) - keep
	start := m.Ctx.CompactBoundary
	if split <= start {
		return nil
	}

	// Forking call: snapshot, release, LLM, reapply.
	toSummarize := make([]types.Message, split-start)
	copy(toSummarize, msgs[start:split])
	prevArchive := m.Ctx.Archive

	rawSummary, err := m.Summarizer(toSummarize, prevArchive)
	if err != nil {
		return fmt.Errorf("compact: %w", err)
	}

	archiveText := FormatCompactSummary(rawSummary)
	if len(strings.TrimSpace(archiveText)) < 50 {
		archiveText = "[Archive unavailable — LLM output malformed]"
	}

	// Quality check: archive must be materially smaller than input.
	inputTokens := token.EstimateTokens(toSummarize)
	archiveTokens := token.EstimateString(archiveText)
	minArchive := inputTokens / 10
	if minArchive < 200 {
		minArchive = 200
	}

	// Constraint verification handled by Memory at Layer 0.

	m.Ctx.Archive = archiveText

	// Facts preserved in Memory by caller.

	if archiveTokens < minArchive {
		keep *= 2
		if keep > len(m.Ctx.Messages)-1 {
			keep = len(m.Ctx.Messages) - 1
		}
		split = len(m.Ctx.Messages) - keep
	}

	m.Ctx.CompactBoundary = split
	debug.Log("full_compact: summarized %d msgs (%d tokens) into archive (%d tokens), boundary=%d, kept=%d",
		len(toSummarize), inputTokens, archiveTokens, split, keep)
	m.trimOldMessages()
	return nil
}

func (m *Compactor) trimOldMessages() {
	const maxPreservedBeforeBoundary = 200
	if m.Ctx.CompactBoundary > maxPreservedBeforeBoundary {
		trim := m.Ctx.CompactBoundary - maxPreservedBeforeBoundary
		m.Ctx.Messages = (m.Ctx.Messages)[trim:]
		m.Ctx.CompactBoundary -= trim
		*m.TrimCount += trim
			debug.Log("trim_old: removed %d messages before boundary (total trimmed: %d)", trim, *m.TrimCount)
	}
}

func (m *Compactor) countMessagesForLastNTurns(n int) int {
	if n <= 0 {
		return 0
	}
	msgs := m.Ctx.Messages
	turns := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			turns++
			if turns >= n {
				return len(msgs) - i
			}
		}
	}
	return len(msgs)
}
