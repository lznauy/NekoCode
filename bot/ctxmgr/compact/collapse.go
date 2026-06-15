package compact

import (
	"nekocode/bot/debug"
	"fmt"
	"strings"

	"nekocode/bot/ctxmgr/token"
	"nekocode/llm/types"
)

// Layer 4: Context Collapsing.
// Lightweight LLM-based compression of the middle segment of message history.
// Unlike Layer 5 (full Head-Tail reconstruction), Collapsing only compresses
// messages between compactBoundary and the most recent 3 turns, fusing the
// result with the existing Archive.
//
// Result is stored in Ctx.Archive (Layer 0.5) — stable between collapses.

// CollapseContext compresses the middle-segment messages into the Archive.
// It preserves the most recent 3 turns intact (Head) and compresses everything
// between compactBoundary and Head into a fused Archive.
func (m *Compactor) CollapseContext() error {
	if m.Summarizer == nil {
		return nil
	}

	tailKeep := m.countMessagesForLastNTurns(3)
	visible := m.Ctx.Messages[m.Ctx.CompactBoundary:]
	if len(visible) <= tailKeep {
		return nil // nothing to collapse
	}

	// Messages to compress: from boundary up to (but not including) Head.
	collapseCount := len(visible) - tailKeep
	if collapseCount <= 0 {
		return nil
	}

	toCollapse := make([]types.Message, collapseCount)
	copy(toCollapse, visible[:collapseCount])

	// Build the prompt: fuse existing Archive + new messages.
	var prompt strings.Builder
	prompt.WriteString(NO_TOOLS_PREAMBLE)
	prompt.WriteString("\n\nFuse the previous archive (if any) with the new messages below into a single, self-contained archive.\n")
	prompt.WriteString("Preserve: full code snippets, exact error messages, file paths with line numbers, user constraints.\n")
	prompt.WriteString("Remove: superseded information, resolved errors, redundant content.\n\n")

	if m.Ctx.Archive != "" {
		prompt.WriteString("=== Previous archive ===\n")
		prompt.WriteString(m.Ctx.Archive)
		prompt.WriteString("\n\n")
	}

	prompt.WriteString("=== New messages to merge ===\n")
	prompt.WriteString(FormatMessages(toCollapse))

	rawSummary, err := m.Summarizer([]types.Message{{Role: "user", Content: prompt.String()}}, "")
	if err != nil {
		return fmt.Errorf("collapse: %w", err)
	}

	archive := FormatCompactSummary(rawSummary)
	if len(strings.TrimSpace(archive)) < 50 {
		return nil // too small to be useful, skip
	}

	// Quality check: archive should be smaller than what it replaces.
	collapseTokens := token.EstimateTokens(toCollapse)
	archiveTokens := token.EstimateString(archive)
	if archiveTokens > collapseTokens*8/10 {
		return nil // archive not meaningfully smaller
	}

	m.Ctx.Archive = archive
	m.Ctx.CompactBoundary += collapseCount
	debug.Log("collapse: compressed %d msgs (%d tokens) into archive (%d tokens), boundary now %d",
		collapseCount, collapseTokens, archiveTokens, m.Ctx.CompactBoundary)
	m.trimOldMessages()
	return nil
}
