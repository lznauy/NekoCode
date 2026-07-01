package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/common"
)

const steeringChBuffer = 4

type lifecycleState struct {
	mu        sync.Mutex
	parentCtx context.Context
	curCtx    context.Context
	cancelFn  context.CancelFunc
	steering  chan string
	startTime time.Time
	finished  atomic.Bool
}

func newLifecycleState(parent context.Context) lifecycleState {
	ctx, cancel := context.WithCancel(parent)
	return lifecycleState{
		parentCtx: parent,
		curCtx:    ctx,
		cancelFn:  cancel,
		steering:  make(chan string, steeringChBuffer),
	}
}

func (s *lifecycleState) context() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.curCtx
}

func (s *lifecycleState) replaceContext() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelFn()
	s.curCtx, s.cancelFn = context.WithCancel(s.parentCtx)
}

func (s *lifecycleState) resetContextIfCanceled() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.curCtx.Err() != nil {
		s.curCtx, s.cancelFn = context.WithCancel(s.parentCtx)
	}
}

func (s *lifecycleState) cancel() {
	s.mu.Lock()
	s.cancelFn()
	s.mu.Unlock()
}

func (s *lifecycleState) duration() time.Duration {
	if s.startTime.IsZero() {
		return 0
	}
	return time.Since(s.startTime)
}

func (s *lifecycleState) start() {
	s.finished.Store(false)
	s.startTime = time.Now()
}

type runState struct {
	step       int
	stopReason hooks.StopReason
	lastText   string
	finalText  string

	consecutiveHints    int
	consecutiveFailures int
	pendingHints        []hooks.Hint
	gate                *responseGate
}

func newRunState() runState {
	return runState{
		gate: newResponseGate(),
	}
}

func (s *runState) reset() {
	gate := s.gate
	*s = runState{
		stopReason: hooks.StopCompleted,
	}
	if gate != nil {
		s.gate = gate
		s.gate.Reset()
	} else {
		s.gate = newResponseGate()
	}
}

const defaultMaxRetries = 2

type responseGate struct {
	MaxRetries int
	retries    int
}

func newResponseGate() *responseGate {
	return &responseGate{MaxRetries: defaultMaxRetries}
}

func (g *responseGate) TryRetry(reason string) (retry bool, hint string) {
	g.retries++
	if g.retries > g.MaxRetries {
		debug.Log("[GOVERNANCE] response gate: retries exhausted (%d/%d), allowing through: %s",
			g.retries-1, g.MaxRetries, reason)
		return false, ""
	}
	return true, reason
}

func (g *responseGate) Reset() { g.retries = 0 }

type streamState struct {
	phase      common.PhaseFunc
	text       StreamCallback
	reasoning  ReasoningCallback
	lastReason string
}

func (s *streamState) resetReasoning() {
	s.lastReason = ""
}

func (s *streamState) emitPhase(phase string) {
	if s.phase != nil {
		s.phase(phase)
	}
}

func (s *streamState) emitText(delta string) {
	if s.text != nil {
		s.text(delta, false)
	}
}

func (s *streamState) emitReasoning(delta string) {
	if s.reasoning != nil {
		s.reasoning(delta)
	}
}

type tokenMeter struct {
	prompt     atomic.Int64
	completion atomic.Int64
	promptSnap int64
	complSnap  int64
}

func (m *tokenMeter) add(prompt, completion int) {
	m.prompt.Add(int64(prompt))
	m.completion.Add(int64(completion))
}

func (m *tokenMeter) total(contextTokens int) (prompt, completion int) {
	return contextTokens, int(m.completion.Load())
}

func (m *tokenMeter) turn(contextTokens int) (prompt, completion int) {
	return contextTokens - int(m.promptSnap), int(m.completion.Load() - m.complSnap)
}

func (m *tokenMeter) snapshot(contextTokens int) {
	m.promptSnap = int64(contextTokens)
	m.complSnap = m.completion.Load()
}
