package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/bot/agent/runtime/control"
	"nekocode/bot/agent/runtime/subagents"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"

	"nekocode/common"
)

type StreamCallback func(delta string, isToolCall bool)
type ReasoningCallback func(delta string)

const steeringChBuffer = 4

type Agent struct {
	// Lifecycle.
	liveMu    sync.Mutex
	parentCtx context.Context
	curCtx    context.Context
	cancelFn  context.CancelFunc
	steering  chan string
	startTime time.Time
	finished  atomic.Bool

	// Dependencies.
	ctxMgr       *ctxmgr.Manager
	llmClient    types.LLM
	toolRegistry *tools.Registry
	executor     *tools.Executor
	subSlotMgr   *subagents.SlotManager
	gov          *aggov.Manager

	// Callbacks.
	phase      common.PhaseFunc
	textFn     StreamCallback
	reasonFn   ReasoningCallback
	lastReason string

	// Token accounting.
	promptTok  atomic.Int64
	complTok   atomic.Int64
	promptSnap int64
	complSnap  int64

	// Current run state.
	step       int
	stopReason hooks.StopReason
	lastText   string
	finalText  string

	consecutiveHints    int
	consecutiveFailures int
	pendingHints        []hooks.Hint
	gate                *control.ResponseGate
}

func New(ctx context.Context, ctxMgr *ctxmgr.Manager, llmClient types.LLM, toolRegistry *tools.Registry) *Agent {
	agentCtx, cancel := context.WithCancel(ctx)
	return &Agent{
		parentCtx:    ctx,
		curCtx:       agentCtx,
		cancelFn:     cancel,
		steering:     make(chan string, steeringChBuffer),
		ctxMgr:       ctxMgr,
		llmClient:    llmClient,
		toolRegistry: toolRegistry,
		executor:     tools.NewExecutor(toolRegistry),
		subSlotMgr:   subagents.NewSlotManager(),
		gate:         control.NewResponseGate(),
	}
}

func (a *Agent) getCtx() context.Context {
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	return a.curCtx
}
func (a *Agent) replaceCtx() {
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	a.cancelFn()
	a.curCtx, a.cancelFn = context.WithCancel(a.parentCtx)
}

func (a *Agent) Steer(msg string) {
	debug.Log("Steer: msg=%q", msg)
	select {
	case a.steering <- msg:
	default:
	}
	a.replaceCtx()
	debug.Log("Steer: context replaced")
}

func (a *Agent) Abort() {
	debug.Log("Abort: user interrupt requested")
	a.finished.Store(true)
	a.liveMu.Lock()
	a.cancelFn()
	a.liveMu.Unlock()
}

func (a *Agent) Duration() time.Duration {
	if a.startTime.IsZero() {
		return 0
	}
	return time.Since(a.startTime)
}

func (a *Agent) Reset() {
	a.liveMu.Lock()
	if a.curCtx.Err() != nil {
		a.curCtx, a.cancelFn = context.WithCancel(a.parentCtx)
	}
	a.step = 0
	a.stopReason = hooks.StopCompleted
	a.lastReason = ""
	a.lastText = ""
	a.finalText = ""
	a.consecutiveHints = 0
	a.consecutiveFailures = 0
	a.pendingHints = nil
	a.liveMu.Unlock()

	a.finished.Store(false)
	a.promptSnap = int64(a.ContextTokens())
	a.complSnap = a.complTok.Load()
	a.startTime = time.Now()
	if a.gov != nil {
		a.gov.Reset()
	}
	a.ctxMgr.SetTodos(nil)
	a.ctxMgr.SetHints("")
}
