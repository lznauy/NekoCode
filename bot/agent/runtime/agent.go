package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
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

	step       int
	finished   atomic.Bool
	stopReason hooks.StopReason
	gov        *govManager
	lastText   string
	finalText  string

	consecutiveHints    int
	consecutiveFailures int
	pendingHints        []hooks.Hint

	transform ContextTransform

	startTime time.Time
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

func (a *Agent) SetGovernanceManager(gov *govManager) { a.gov = gov }
func (a *Agent) GovernanceManager() *govManager       { return a.gov }

// SetHookRegistry wires the hook registry into the agent's govManager.
// If no manager exists yet, one is created.
func (a *Agent) SetHookRegistry(m *hooks.Registry) {
	if a.gov == nil {
		a.gov = newGovManager(m)
	} else {
		a.gov.HookReg = m
	}
}

func (a *Agent) SetConfirmFn(fn common.ConfirmFunc) { a.executor.SetConfirmFn(fn) }
func (a *Agent) ConfirmFn() common.ConfirmFunc      { return a.executor.ConfirmFn() }
func (a *Agent) SetPhaseFn(fn common.PhaseFunc)     { a.phase = fn; a.executor.SetPhaseFn(fn) }
func (a *Agent) PhaseFn() common.PhaseFunc          { return a.phase }
func (a *Agent) SetPlanMode(on bool)                { a.executor.SetPlanMode(on) }

func (a *Agent) ToolExecutionState() *tools.ExecutionState {
	return a.executor.ExecutionState()
}

func (a *Agent) WireTodoWrite(fn common.TodoFunc) {
	if t, err := a.toolRegistry.Get("todo_write"); err == nil {
		if updater, ok := t.(interface{ SetUpdateFn(common.TodoFunc) }); ok {
			updater.SetUpdateFn(fn)
		}
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

// GovernanceLine returns a one-line governance status summary for /context.
func (a *Agent) GovernanceLine() string {
	if a.gov == nil {
		return ""
	}
	return a.gov.Summary()
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
	a.promptSnap = a.promptTok.Load()
	a.complSnap = a.complTok.Load()
	a.startTime = time.Now()
	if a.gov != nil {
		a.gov.Reset()
	}
	a.ctxMgr.SetTodos(nil)
	a.ctxMgr.SetHints("")
}
