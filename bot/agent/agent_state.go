package agent

import (
	"sync/atomic"

	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

const steeringChBuffer = 4

const defaultMaxRetries = 2

// runState holds the mutable state of a single Run.
type runState struct {
	hookStopped bool
	// Progress and termination.
	step       int               // current iteration, capped by maxAgentSteps
	stopReason policy.StopReason // why the run stopped; finishRun branches on it
	err        error             // unrecoverable preparation failure

	// lastText is the latest model text. finalText holds an accepted final
	// answer; rejected candidates are cleared. finalPersisted tracks whether
	// that answer has already been appended to the context manager.
	lastText       string
	finalText      string
	finalPersisted bool

	// Loop-stuck guards: consecutive Stop hint injections (capped by
	// maxConsecutiveHints) and consecutive LLM failures (maxConsecutiveFailures).
	consecutiveHints    int
	consecutiveFailures int

	// Hints staged across phases (UserSubmit/PostToolBatch), merged into the next
	// turn's transient hints.
	pendingHints []policy.Hint
}

func (s *runState) reset() {
	*s = runState{stopReason: policy.StopCompleted}
}

// clearFinalState drops the recorded final/last answer, e.g. when the run
// ends without a valid final text (step limit, hook stop, repeated errors).
func (a *Agent) clearFinalState() {
	a.run.lastText = ""
	a.run.finalText = ""
	a.run.finalPersisted = false
}

// streamState is the agent's fine-grained streaming output channel,
// complementing RunCallback (coarse step events). The three callbacks push
// phase labels, answer-text deltas, and reasoning deltas to the application
// host while the LLM response streams in. Provider-issued reasoning belongs to
// one response and is carried by reasoningResult rather than retained here.
type streamState struct {
	phase     protocol.PhaseFunc
	text      func(string)
	reasoning func(string)
	resetText func()
}

func (s *streamState) emitPhase(phase string) {
	if s.phase != nil {
		s.phase(phase)
	}
}

func (s *streamState) emitText(delta string) {
	if s.text != nil {
		s.text(delta)
	}
}

// discardText clears the host's streamed answer text for the current turn.
func (s *streamState) discardText() {
	if s.resetText != nil {
		s.resetText()
	}
}

func (s *streamState) emitReasoning(delta string) {
	if s.reasoning != nil {
		s.reasoning(delta)
	}
}

// tokenMeter accumulates token usage for an agent. Prompt tokens are not
// tracked here — the caller passes the live context occupancy from ctxMgr;
// only completion tokens are accumulated, from LLM stream callbacks (hence
// the atomics: streaming callbacks and the loop run concurrently).
type tokenMeter struct {
	completion atomic.Int64
	promptSnap atomic.Int64
	complSnap  atomic.Int64
}

// llmUsageMeter aggregates provider-reported usage for the current run.
// Unlike tokenMeter, these values describe actual LLM requests and preserve
// whether every input reported a cache split.
type llmUsageMeter struct {
	input        atomic.Int64
	cached       atomic.Int64
	newTokens    atomic.Int64
	output       atomic.Int64
	reasoning    atomic.Int64
	reported     atomic.Int64
	unknownCache atomic.Int64
}

func (m *llmUsageMeter) reset() {
	m.input.Store(0)
	m.cached.Store(0)
	m.newTokens.Store(0)
	m.output.Store(0)
	m.reasoning.Store(0)
	m.reported.Store(0)
	m.unknownCache.Store(0)
}

func (m *llmUsageMeter) record(usage types.StreamUsage) {
	if usage.PromptTokens <= 0 && usage.CompletionTokens <= 0 {
		return
	}
	m.input.Add(int64(max(0, usage.PromptTokens)))
	m.output.Add(int64(max(0, usage.CompletionTokens)))
	m.reasoning.Add(int64(max(0, min(usage.ReasoningTokens, usage.CompletionTokens))))
	if usage.CacheUsageReported {
		m.cached.Add(int64(max(0, usage.CacheHitTokens)))
		m.newTokens.Add(int64(max(0, usage.CacheMissTokens)))
		m.reported.Add(1)
	} else if usage.PromptTokens > 0 {
		m.unknownCache.Add(1)
	}
}

func (m *llmUsageMeter) snapshot() (total, input, cached, fresh, output, reasoning int, cacheReported bool) {
	input = int(m.input.Load())
	cached = int(m.cached.Load())
	fresh = int(m.newTokens.Load())
	output = int(m.output.Load())
	total = input + output
	reasoning = min(int(m.reasoning.Load()), output)
	cacheReported = m.reported.Load() > 0 && m.unknownCache.Load() == 0
	return
}

func (m *tokenMeter) addCompletion(completion int) {
	m.completion.Add(int64(completion))
}

// total reports cumulative usage: live context occupancy plus all
// completion tokens seen so far.
func (m *tokenMeter) total(contextTokens int) (prompt, completion int) {
	return contextTokens, int(m.completion.Load())
}

// turn reports usage since the last snapshot (i.e. the current run).
func (m *tokenMeter) turn(contextTokens int) (prompt, completion int) {
	return contextTokens - int(m.promptSnap.Load()), int(m.completion.Load() - m.complSnap.Load())
}

// snapshot records the baseline for turn() at the start of a run.
func (m *tokenMeter) snapshot(contextTokens int) {
	m.promptSnap.Store(int64(contextTokens))
	m.complSnap.Store(m.completion.Load())
}
