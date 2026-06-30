package runtime

import (
	"context"
	"time"

	"nekocode/bot/agent/runtime/modelrun"
	"nekocode/bot/agent/runtime/toolrun"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/debug"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
)

type StreamCallback func(delta string, isToolCall bool)
type ReasoningCallback func(delta string)

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
	modelRunner *modelrun.Runner
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
	a.modelRunner = modelrun.New(host)
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
