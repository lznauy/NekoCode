package compact

import (
	"context"
	"fmt"

	"nekocode/bot/debug"

	ctxctx "nekocode/bot/contextmgr/context"
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/llm/types"
)

// defaultBudget is the fallback context window size when not configured.
const defaultBudget = 64000

// Summarizer is the function signature for LLM summarization.
type Summarizer func(msgs []types.Message, prevSummary string) (string, error)

// Tracker provides token estimates for compaction decisions.
type Tracker interface {
	PromptEstimate() int
	Total() int
}

// Compactor holds references to the parent's state for compaction.
type Compactor struct {
	Ctx           *ctxctx.Content
	ContextWindow *int
	Tracker       Tracker

	CompactCount *int
	TrimCount    *int

	Summarizer Summarizer
	CancelCtx  context.Context // cancellable context for LLM calls
	Cfg        Config
}

// -- 5-layer compaction pipeline ---------------------------------------

// AutoCompactIfNeeded runs the compaction pipeline. Each layer is tried in
// order; after each layer, tokens are re-estimated. If context is back
// within safe bounds, the pipeline stops.
//
//	Layer 2: History Sniping     (cold messages before boundary, ~free)
//	Layer 3: Microcompact        (surgical tool-result clearing)
//	Layer 4: Context Collapsing  (LLM middle-segment → Archive)
//	Layer 5: Auto-Compaction     (Head-Tail-Summary reconstruction)
//
// estimateTokens returns the higher of visibleEstimatedTokens and Tracker.PromptEstimate.
func (m *Compactor) estimateTokens() int {
	est := m.visibleEstimatedTokens()
	if t := m.Tracker.PromptEstimate(); t > est {
		return t
	}
	return est
}

func (m *Compactor) AutoCompactIfNeeded() (Level, error) {
	estTokens := m.estimateTokens()
	effectiveBudget := m.effectiveBudget()
	cfg := m.effectiveConfig()
	remaining := effectiveBudget - estTokens
	level := classifyLevel(remaining, cfg)

	if level == LevelNormal || level == LevelWarning {
		return level, nil
	}
	debug.Log("auto_compact: level=%s est=%d budget=%d remaining=%d msgs=%d",
		level.String(), estTokens, effectiveBudget, remaining, len(m.Ctx.Messages))
	if level == LevelBlocking {
		return level, fmt.Errorf("context full: %d tokens used of %d budget (only %d remaining)",
			estTokens, effectiveBudget, remaining)
	}

	// --- Layer 2: History Sniping ---
	// Always try snipe on cold history — it's free and targets messages
	// that are already excluded from Build() output.
	if snipped := m.SnipHistory(); snipped > 0 {
		if m.recheckBudget(effectiveBudget, cfg) == LevelNormal {
			return LevelMicroCompact, nil
		}
	}

	// --- Layer 3: Microcompact ---
	// Level ordering: Normal < Warning < MicroCompact < Compact < Blocking
	if level <= LevelMicroCompact {
		if cleared := m.MicroCompactIfNeeded(); cleared > 0 {
			return LevelMicroCompact, nil
		}
	}

	// --- Layer 4: Context Collapsing ---
	if level <= LevelCompact {

		// Lightweight collapse: compress middle segment into Archive.
		if err := m.CollapseContext(); err == nil {
			if m.recheckBudget(effectiveBudget, cfg) <= LevelWarning {
				return LevelCompact, nil
			}
		}
	}

	// --- Layer 5: Auto-Compaction ---
	// Full Head-Tail-Summary reconstruction.
	if err := m.FullCompact(); err != nil {
		return LevelCompact, fmt.Errorf("auto compact failed: %w", err)
	}
	return LevelCompact, nil
}

// recheckBudget re-estimates tokens and returns the current level.
func (m *Compactor) recheckBudget(effectiveBudget int, cfg Config) Level {
	return classifyLevel(effectiveBudget-m.estimateTokens(), cfg)
}

// -- budget estimation -------------------------------------------------

func (m *Compactor) visibleEstimatedTokens() int {
	visible := m.Ctx.Messages
	if m.Ctx.CompactBoundary > 0 && m.Ctx.CompactBoundary < len(visible) {
		visible = visible[m.Ctx.CompactBoundary:]
	}
	n := token.EstimateTokens(visible)
	if m.Ctx.Archive != "" {
		n += token.EstimateString(m.Ctx.Archive)
	}
	return n
}

// -- public entry points (called by ctxmgr.Manager) --------------------

func (m *Compactor) NeedsSummarization() bool {
	if m.Summarizer == nil || len(m.Ctx.Messages) <= 20 {
		return false
	}
	if m.visibleEstimatedTokens() > m.effectiveBudget()*8/10 {
		return true
	}
	return false
}

func (m *Compactor) MicroCompactIfNeeded() int {
	if m.estimateTokens() < m.effectiveBudget()/2 {
		return 0
	}
	return m.microCompact()
}

// effectiveBudget returns the token budget, defaulting to defaultBudget if unset.
func (m *Compactor) effectiveBudget() int {
	if *m.ContextWindow > 0 {
		return *m.ContextWindow
	}
	return defaultBudget
}

// effectiveConfig scales thresholds for the actual budget.
func (m *Compactor) effectiveConfig() Config {
	budget := *m.ContextWindow
	if budget <= 0 {
		budget = defaultBudget
	}
	if budget <= defaultBudget {
		return m.Cfg
	}
	scale := float64(budget) / float64(defaultBudget)
	return Config{
		WarningBuffer:      int(float64(m.Cfg.WarningBuffer) * scale),
		MicroCompactBuffer: int(float64(m.Cfg.MicroCompactBuffer) * scale),
		CompactBuffer:      int(float64(m.Cfg.CompactBuffer) * scale),
		BlockingBuffer:     int(float64(m.Cfg.BlockingBuffer) * scale),
	}
}
