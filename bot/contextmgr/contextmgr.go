// Package contextmgr provides a layered context management system for LLM conversations.
package contextmgr

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"nekocode/bot/calllog"
	"nekocode/bot/contextmgr/memory"
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/decision"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

type Manager struct {
	state *managerState

	// compactionMu serializes slow summary operations without blocking normal
	// context reads and writes for the duration of the model call.
	compactionMu sync.Mutex

	// runtimePrompt is evaluated for every Build and excluded from snapshots.
	runtimePrompt func() string

	usageMu            sync.RWMutex
	usageRecorder      func(types.StreamUsage)
	compactionObserver func(protocol.CompactionEvent)
	sessionIDProvider  func() string
	beforeCompaction   func(string) error
	afterCompaction    func()
	toolPruner         decision.ToolPruner
}

type managerState struct {
	mu sync.RWMutex

	ctx           contextContent
	contextWindow int
	tracker       *token.Tracker
	prefix        prefixTracker
	revision      uint64
	compactCount  int
	trimCount     int
	compressor    *replacementCompactor
	reasoning     types.ReasoningSettings
	// Append-only projections remember their latest provider-visible value so
	// unchanged controller state does not add noise to the cached prefix.
	runtimeProjection appendProjection
	hintProjection    appendProjection
	// runtimePolicy is controller-owned, per-run policy state (for example
	// plan mode). BuildRequest folds it into the next tagged runtime snapshot.
	runtimePolicy string
	// restoredTurn is the last turn recorded in the session file. It is shown
	// only until this process completes a turn of its own, so a reloaded
	// session keeps reporting the previous turn instead of going blank.
	restoredTurn PrefixTurnStats
	// transcript is the append-only, user-visible conversation. Compaction and
	// context repair mutate ctx.Messages but never this record.
	transcript []types.Message
}

type appendProjection struct {
	last string
	seen bool
}

func (p *appendProjection) changed(value string) bool {
	if p.seen && p.last == value {
		return false
	}
	p.last, p.seen = value, true
	return true
}

func (p *appendProjection) reset() { *p = appendProjection{} }

type Config struct {
	SystemPrompt       string
	ContextWindow      int
	AutoCompactPercent int
	Memory             *memory.File
	Summarizer         Summarizer
	CompactionModel    provider.LLM
	Reasoning          types.ReasoningSettings
	RuntimePrompt      func() string
	// ToolPruner is an optional fail-open relevance judge consulted before
	// compaction summarizes old tool results. nil (the default) keeps the
	// unpruned behavior; the capability-enhancement entry for decision
	// engines such as Jev.
	ToolPruner decision.ToolPruner
}

var writeCompactionRecord = calllog.Write

// SetToolPruner applies configuration changes to subsequent compactions.
func (m *Manager) SetToolPruner(pruner decision.ToolPruner) {
	m.usageMu.Lock()
	m.toolPruner = pruner
	m.usageMu.Unlock()
}

func (m *Manager) makeSummarizer(ctx context.Context, client provider.LLM) Summarizer {
	return m.streamingSummarizer(ctx, client, nil)
}

func (m *Manager) streamingSummarizer(ctx context.Context, client provider.LLM, delta func(string)) Summarizer {
	return func(msgs []types.Message, prevSummary string) (string, error) {
		var summary string
		var err error
		for attempt := 0; attempt < 2; attempt++ {
			summary, err = m.summarizeStreamOnce(ctx, client, msgs, prevSummary, delta)
			// Any non-empty result is final: its deltas were already streamed
			// to the UI, so a retry would duplicate them. Only a fully empty
			// response (provider closed the stream without content — often
			// transient) is retried once. A cancelled context ends the loop.
			if strings.TrimSpace(summary) != "" || ctx.Err() != nil {
				return summary, err
			}
		}
		if err == nil {
			if source, ok := client.(interface{ RequestMeta() types.RequestMeta }); ok {
				meta := source.RequestMeta()
				err = fmt.Errorf("summarizer model %q returned no content (retried once); check the flash/compaction model", meta.Model)
			} else {
				err = fmt.Errorf("summarizer returned no content (retried once)")
			}
		}
		return summary, err
	}
}

func (m *Manager) summarizeStreamOnce(ctx context.Context, client provider.LLM, msgs []types.Message, prevSummary string, delta func(string)) (string, error) {
	start := time.Now()
	tokens, errs := client.ChatStream(ctx, buildSummaryMessages(msgs, prevSummary), nil)
	var summary strings.Builder
	var usage types.StreamUsage
	var err error
	cancelled := ctx.Done()
	for tokens != nil || errs != nil {
		select {
		case <-cancelled:
			err = ctx.Err()
			// Providers may already be blocked sending a token. Drain their
			// cancelled request to closure and retain final usage accounting.
			cancelled = nil
		case tok, ok := <-tokens:
			if !ok {
				tokens = nil
				continue
			}
			if tok.Usage != nil {
				usage.Merge(tok.Usage)
			}
			if tok.Content != "" && ctx.Err() == nil {
				summary.WriteString(tok.Content)
				if delta != nil {
					delta(tok.Content)
				}
			}
		case streamErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if streamErr != nil {
				err = streamErr
			}
		}
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && strings.TrimSpace(summary.String()) == "" {
		err = fmt.Errorf("no response from summarizer")
	}
	usage.Normalize()
	m.recordLLMUsage(usage)
	m.writeCompactionCall(client, usage, start, err)
	return summary.String(), err
}

func (m *Manager) writeCompactionCall(client provider.LLM, usage types.StreamUsage, start time.Time, callErr error) {
	rec := calllog.Record{TS: time.Now(), Source: "compaction", DurMs: time.Since(start).Milliseconds()}
	m.usageMu.RLock()
	sessionIDProvider := m.sessionIDProvider
	m.usageMu.RUnlock()
	if sessionIDProvider != nil {
		rec.SessionID = sessionIDProvider()
	}
	rec.SetUsage(usage)
	if source, ok := client.(interface{ RequestMeta() types.RequestMeta }); ok {
		meta := source.RequestMeta()
		rec.Model = meta.Model
		rec.Protocol = meta.Protocol
		rec.BaseURL = calllog.SafeBaseURL(meta.BaseURL)
		rec.RequestedEffort = meta.RequestedEffort
		rec.EffectiveEffort = meta.EffectiveEffort
	}
	rec.Err = calllog.ErrorSummary(callErr)
	writeCompactionRecord(rec)
}

// SetSessionIDProvider scopes compaction call logs to this manager instance.
func (m *Manager) SetSessionIDProvider(provider func() string) {
	m.usageMu.Lock()
	m.sessionIDProvider = provider
	m.usageMu.Unlock()
}

func (m *Manager) recordLLMUsage(usage types.StreamUsage) {
	if usage.PromptTokens <= 0 && usage.CompletionTokens <= 0 {
		return
	}
	m.usageMu.RLock()
	recorder := m.usageRecorder
	m.usageMu.RUnlock()
	if recorder != nil {
		recorder(usage)
	}
}

// SetLLMUsageRecorder connects compaction calls to the owning
// run's token meter. It does not affect the per-call JSONL record.
func (m *Manager) SetLLMUsageRecorder(recorder func(types.StreamUsage)) {
	m.usageMu.Lock()
	m.usageRecorder = recorder
	m.usageMu.Unlock()
}

func New(cfg Config) *Manager {
	ctx := newContextContent(cfg.SystemPrompt)
	if cfg.Memory != nil {
		ctx.Memory = cfg.Memory.Build()
	}
	m := &Manager{state: &managerState{
		ctx:           ctx,
		tracker:       &token.Tracker{},
		contextWindow: cfg.ContextWindow,
		reasoning:     cfg.Reasoning,
	}, runtimePrompt: cfg.RuntimePrompt, toolPruner: cfg.ToolPruner}
	summarizer := cfg.Summarizer
	if summarizer == nil && cfg.CompactionModel != nil {
		summarizer = m.makeSummarizer(context.Background(), cfg.CompactionModel)
	}
	m.initCompressor(summarizer, cfg.AutoCompactPercent)
	if cfg.Summarizer == nil {
		m.state.compressor.model = cfg.CompactionModel
	}
	return m
}

const defaultAutoCompactPercent = 80

func normalizeAutoCompactPercent(percent int) int {
	if percent < 1 || percent > 99 {
		return defaultAutoCompactPercent
	}
	return percent
}

func (m *Manager) initCompressor(summarizer Summarizer, autoCompactPercent int) {
	m.state.compressor = newReplacementCompactor(summarizer, autoCompactPercent)
}
