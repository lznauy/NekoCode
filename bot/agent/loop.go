package agent

import (
	"errors"

	"nekocode/bot/agent/internal/kernel"
	"nekocode/bot/contextmgr"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/protocol"
)

// maxAgentSteps is the hard ceiling on agent loop iterations. Prevents
// infinite loops when the LLM keeps producing tool calls or Stop hooks
// keep injecting hints without progress.
const maxAgentSteps = 150

// maxConsecutiveHints is the maximum number of consecutive Stop hint
// injections without any intervening tool calls. Beyond this the agent
// is stuck in a text-only loop and should stop.
const maxConsecutiveHints = 3

// maxConsecutiveFailures is the maximum number of consecutive LLM call
// failures (after retries). Beyond this the LLM is likely broken.
const maxConsecutiveFailures = 5

const msgInterrupted = "Interrupted"

type RunResult struct {
	FinalOutput string
	Error       error
	Steps       int
	Interrupted bool
}

type RunCallback func(ev protocol.StepEvent)

type loopRunner struct {
	agent *Agent
}

func newLoopRunner(agent *Agent) *loopRunner {
	return &loopRunner{agent: agent}
}

func (r *loopRunner) run(input string, callback RunCallback) *RunResult {
	a := r.agent
	beforeRun := a.deps.ctxMgr.Snapshot()
	r.startRun(input)
	defer r.logGovernanceSummary()
	r.applyUserSubmitHooks()

	kernel.RunLoop(kernel.Loop{
		Done:             func() bool { return a.life.Finished().Load() },
		StepLimitReached: r.stepLimitReached,
		Step:             func() bool { return r.runTurn(input, callback) },
		FinishStep:       func() { a.life.Finished().Store(true) },
	})
	result := r.finishRun(callback)
	// Flush any steering messages that arrived too late to be drained during
	// the run, so they are still recorded in the context instead of being lost.
	result.Error = errors.Join(result.Error, a.drainSteering())
	r.restoreInterruptedRun(beforeRun, result)
	return result
}

func (r *loopRunner) restoreInterruptedRun(before contextmgr.ManagerSnapshot, result *RunResult) {
	if result != nil && result.Interrupted {
		r.agent.deps.ctxMgr.Restore(before)
	}
}

func (r *loopRunner) runTurn(input string, callback RunCallback) (finished bool) {
	a := r.agent
	msgCountBefore := a.deps.ctxMgr.Len()

	if err := a.turnRunner.prepareTurn(input); err != nil {
		a.run.err = err
		a.clearFinalState()
		return true
	}
	defer a.deps.ctxMgr.SetHints("")

	if a.turnRunner.interruptedBeforeReasoning(callback) {
		return true
	}

	reasoning := a.modelRunner.reason(input)
	if a.turnRunner.retryAfterInterruptedReasoning(reasoning, msgCountBefore) {
		return false
	}
	if a.run.stopReason == policy.StopInterrupted {
		return true
	}

	if len(reasoning.ToolCalls) > 0 {
		return a.turnRunner.handleToolCalls(reasoning.ToolCalls, reasoning, callback)
	}

	return a.turnRunner.handleText(reasoning, callback)
}

func (r *loopRunner) startRun(input string) {
	a := r.agent
	a.Reset()
	a.deps.ctxMgr.BeginModelTurn()
	a.deps.ctxMgr.Add("user", input, "user")
}

func (r *loopRunner) applyUserSubmitHooks() {
	a := r.agent
	if a.deps.gov == nil {
		return
	}
	for _, h := range collectHints(a.deps.gov.OnUserSubmit()) {
		h := h
		a.injectHint(&h)
	}
}

func (r *loopRunner) logGovernanceSummary() {
	a := r.agent
	if a.deps.gov == nil {
		return
	}
	logger.Log("[GOVERNANCE] task complete: steps=%d, %s", a.run.step, a.deps.gov.Summary())
}

func (r *loopRunner) stepLimitReached() bool {
	a := r.agent
	if a.run.step < maxAgentSteps {
		return false
	}
	a.run.stopReason = policy.StopCompleted
	a.clearFinalState()
	return true
}

func (r *loopRunner) finishRun(callback RunCallback) *RunResult {
	a := r.agent
	if a.getCtx().Err() != nil || a.run.stopReason == policy.StopInterrupted {
		return &RunResult{FinalOutput: msgInterrupted, Steps: a.run.step, Interrupted: true}
	}
	if a.run.err != nil {
		return &RunResult{Error: a.run.err, Steps: a.run.step}
	}
	return &RunResult{FinalOutput: r.resolveFinalOutput(callback), Steps: a.run.step}
}

// resolveFinalOutput picks the run's answer by priority: the recorded final
// text, else the latest text as display fallback, else a synthesized summary.
func (r *loopRunner) resolveFinalOutput(callback RunCallback) string {
	a := r.agent
	if a.run.stopReason == policy.StopCompleted {
		if a.run.finalText != "" {
			// Persist the final answer if it hasn't been already. Recordable
			// chat turns set finalPersisted=true; policy-block paths leave it
			// false, so the text the user saw would otherwise be absent from
			// the saved session.
			if !a.run.finalPersisted {
				a.deps.ctxMgr.AddAssistant(types.Message{Role: "assistant", Content: a.run.finalText})
				a.run.finalPersisted = true
			}
			return a.run.finalText
		}
		if a.run.lastText != "" {
			return a.run.lastText
		}
	}
	output := a.modelRunner.synthesize()
	a.run.finalPersisted = true
	if callback != nil {
		callback(protocol.StepEvent{Action: protocol.StepActionChat, Output: output})
	}
	return output
}
