package runtime

import (
	"context"
	"time"

	"nekocode/bot/agent/runtime/model"
	"nekocode/bot/agent/runtime/toolrun"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/common/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"
	"nekocode/common"
)

type StreamCallback func(delta string, isToolCall bool)
type ReasoningCallback func(delta string)

type ActionType = model.ActionType
type ReasoningResult = model.Result

const (
	ActionChat        = model.ActionChat
	ActionExecuteTool = model.ActionExecuteTool
)

type agentDeps struct {
	ctxMgr       *ctxmgr.Manager
	llmClient    types.LLM
	toolRegistry *tools.Registry
	executor     *tools.Executor
	subSlotMgr   *toolrun.SlotManager
	gov          *aggov.Manager
}

func newAgentDeps(ctxMgr *ctxmgr.Manager, llmClient types.LLM, toolRegistry *tools.Registry) agentDeps {
	return agentDeps{
		ctxMgr:       ctxMgr,
		llmClient:    llmClient,
		toolRegistry: toolRegistry,
		executor:     tools.NewExecutor(toolRegistry),
		subSlotMgr:   toolrun.NewSlotManager(),
	}
}

type Agent struct {
	// Lifecycle.
	life lifecycleState

	// Dependencies.
	deps agentDeps

	// Streaming callbacks and model reasoning.
	stream streamState

	// Token accounting.
	tokens tokenMeter

	// Current run state.
	run runState

	loopRunner  *loopRunner
	modelRunner *model.Runner
	turnRunner  *turnRunner
	toolRunner  *toolrun.Runner
}

func New(ctx context.Context, ctxMgr *ctxmgr.Manager, llmClient types.LLM, toolRegistry *tools.Registry) *Agent {
	a := &Agent{
		life: newLifecycleState(ctx),
		deps: newAgentDeps(ctxMgr, llmClient, toolRegistry),
		run:  newRunState(),
	}
	host := runnerHost{agent: a}
	a.loopRunner = newLoopRunner(a)
	a.modelRunner = model.New(host)
	a.turnRunner = newTurnRunner(a)
	a.toolRunner = toolrun.New(host)
	return a
}

func (a *Agent) getCtx() context.Context {
	return a.life.context()
}
func (a *Agent) replaceCtx() {
	a.life.replaceContext()
}

func (a *Agent) Steer(msg string) {
	debug.Log("Steer: msg=%q", msg)
	select {
	case a.life.steering <- msg:
	default:
	}
	a.replaceCtx()
	debug.Log("Steer: context replaced")
}

func (a *Agent) Abort() {
	debug.Log("Abort: user interrupt requested")
	a.life.finished.Store(true)
	a.life.cancel()
}

func (a *Agent) Duration() time.Duration {
	return a.life.duration()
}

func (a *Agent) Reset() {
	a.life.resetContextIfCanceled()
	a.stream.resetReasoning()
	a.run.reset()

	a.life.start()
	a.tokens.snapshot(a.ContextTokens())
	if a.deps.gov != nil {
		a.deps.gov.Reset()
	}
	a.deps.ctxMgr.SetTodos(nil)
	a.deps.ctxMgr.SetHints("")
}

func (a *Agent) injectHint(h *hooks.Hint) {
	if h != nil {
		a.run.pendingHints = append(a.run.pendingHints, *h)
	}
}

func (a *Agent) applyTurnHints(hints []hooks.Hint) {
	if len(a.run.pendingHints) > 0 {
		hints = append(hints, a.run.pendingHints...)
		a.run.pendingHints = nil
	}
	a.deps.ctxMgr.SetHints(hooks.FormatHints(hints))
}

func (a *Agent) drainSteering() {
	for {
		select {
		case msg := <-a.life.steering:
			a.deps.ctxMgr.Add("user", msg, "user")
		default:
			return
		}
	}
}

func (a *Agent) SetStreamFn(fn StreamCallback) {
	a.stream.text = fn
}

func (a *Agent) SetReasoningStreamFn(fn ReasoningCallback) {
	a.stream.reasoning = fn
}

func (a *Agent) SetPhaseFn(fn common.PhaseFunc) {
	a.stream.phase = fn
	a.deps.executor.SetPhaseFn(fn)
}

func (a *Agent) PhaseFn() common.PhaseFunc {
	return a.stream.phase
}

func (a *Agent) SetGovernanceManager(gov *aggov.Manager) {
	a.deps.gov = gov
}

func (a *Agent) GovernanceManager() *aggov.Manager {
	return a.deps.gov
}

// SetHookRegistry wires the hook registry into the agent's govManager.
// If no manager exists yet, one is created.
func (a *Agent) SetHookRegistry(m *hooks.Registry) {
	if a.deps.gov == nil {
		a.deps.gov = aggov.NewManager(m)
	} else {
		a.deps.gov.HookReg = m
	}
}

func (a *Agent) AddTokens(prompt, completion int) {
	a.tokens.add(prompt, completion)
}

func (a *Agent) TokenUsage() (prompt, completion int) {
	return a.tokens.total(a.ContextTokens())
}

func (a *Agent) TurnTokenUsage() (prompt, completion int) {
	return a.tokens.turn(a.ContextTokens())
}

func (a *Agent) ContextTokens() int {
	_, tokens, _ := a.deps.ctxMgr.Stats()
	return tokens
}

func (a *Agent) SetConfirmFn(fn common.ConfirmFunc) {
	a.deps.executor.SetConfirmFn(fn)
}

func (a *Agent) ConfirmFn() common.ConfirmFunc {
	return a.deps.executor.ConfirmFn()
}

func (a *Agent) SetPlanMode(on bool) {
	a.deps.executor.SetPlanMode(on)
}

func (a *Agent) ToolExecutionState() *tools.ExecutionState {
	return a.deps.executor.ExecutionState()
}

func (a *Agent) WireTodoWrite(fn common.TodoFunc) {
	if t, err := a.deps.toolRegistry.Get("todo_write"); err == nil {
		if updater, ok := t.(interface{ SetUpdateFn(common.TodoFunc) }); ok {
			updater.SetUpdateFn(fn)
		}
	}
}
