package agent

import (
	"nekocode/bot/debug"
	"context"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/bot/agent/budget"
	"nekocode/bot/ctxmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/bot/tools/builtin"
	"nekocode/llm/types"

	"nekocode/common"
)

type ContextTransform func(messages []types.Message) []types.Message
type StreamCallback func(delta string, isToolCall bool)
type ReasoningCallback func(delta string)

const steeringChBuffer = 4

type Agent struct {
	liveMu    sync.Mutex
	parentCtx context.Context
	curCtx    context.Context
	cancelFn  context.CancelFunc
	steering  chan string

	ctxMgr       *ctxmgr.Manager
	llmClient    types.LLM
	toolRegistry *tools.Registry
	executor     *tools.Executor
	subSlotMgr   *SubSlotManager

	phase      common.PhaseFunc
	textFn     StreamCallback
	reasonFn   ReasoningCallback
	lastReason string

	promptTok  atomic.Int64
	complTok   atomic.Int64
	promptSnap int64
	complSnap  int64

	step              int
	finished          atomic.Bool
	stopReason        hooks.StopReason
	exploration       *budget.ExplorationTracker
	lastText          string

	consecutiveHints    int // PostTurn hint injections without tool progress
	consecutiveFailures int // consecutive LLM call failures

	transform    ContextTransform
	hookReg      *hooks.Registry
	
	startTime    time.Time
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
		subSlotMgr:   NewSubSlotManager(),
	}
}

func (a *Agent) SetHookRegistry(m *hooks.Registry) { a.hookReg = m }

func (a *Agent) SetConfirmFn(fn common.ConfirmFunc) { a.executor.SetConfirmFn(fn) }
func (a *Agent) ConfirmFn() common.ConfirmFunc       { return a.executor.ConfirmFn() }
func (a *Agent) SetPhaseFn(fn common.PhaseFunc)     { a.phase = fn; a.executor.SetPhaseFn(fn) }
func (a *Agent) PhaseFn() common.PhaseFunc          { return a.phase }
func (a *Agent) SetPlanMode(on bool)                { a.executor.SetPlanMode(on) }

func (a *Agent) WireTodoWrite(fn common.TodoFunc) {
	if t, err := a.toolRegistry.Get("todo_write"); err == nil {
		t.(*builtin.TodoWriteTool).SetUpdateFn(fn)
	}
}
func (a *Agent) SetContextTransform(fn ContextTransform)   { a.transform = fn }
func (a *Agent) SetStreamFn(fn StreamCallback)             { a.textFn = fn }
func (a *Agent) SetReasoningStreamFn(fn ReasoningCallback) { a.reasonFn = fn }

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

func (a *Agent) AddTokens(prompt, completion int) {
	a.promptTok.Add(int64(prompt))
	a.complTok.Add(int64(completion))
}

func (a *Agent) TokenUsage() (prompt, completion int) {
	return int(a.promptTok.Load()), int(a.complTok.Load())
}

func (a *Agent) TurnTokenUsage() (prompt, completion int) {
	return int(a.promptTok.Load() - a.promptSnap), int(a.complTok.Load() - a.complSnap)
}

func (a *Agent) ContextTokens() int {
	_, tokens, _ := a.ctxMgr.Stats()
	return tokens
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
	a.consecutiveHints = 0
	a.consecutiveFailures = 0
	a.liveMu.Unlock()

	a.finished.Store(false)
	a.promptSnap = a.promptTok.Load()
	a.complSnap = a.complTok.Load()
	a.startTime = time.Now()
	if a.exploration == nil {
		a.exploration = budget.NewExplorationTracker()
	} else {
		a.exploration.Reset()
	}
	if a.hookReg != nil {
		a.hookReg.ResetSession()
		// Reset per-run flags that shouldn't persist across agent invocations.
		// StoreFileModified: prevents verification hook from firing on every
		//   subsequent turn after a file was modified in a previous run.
		// SetTodos: prevents completion_quality from firing on trivial turns
		//   due to leftover completed tasks from a prior agent invocation.
		a.hookReg.Set(hooks.StoreFileModified, 0)
	}
	a.ctxMgr.SetTodos(nil)
}
