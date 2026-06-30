package runtime

import (
	"nekocode/bot/agent/runtime/messages"
	"nekocode/bot/hooks"
)

// maxAgentSteps is the hard ceiling on agent loop iterations. Prevents
// infinite loops when the LLM keeps producing tool calls or PostTurn hooks
// keep injecting hints without progress.
const maxAgentSteps = 150

// maxConsecutiveHints is the maximum number of consecutive PostTurn hint
// injections without any intervening tool calls. Beyond this the agent
// is stuck in a text-only loop and should stop.
const maxConsecutiveHints = 3

// maxConsecutiveFailures is the maximum number of consecutive LLM call
// failures (after retries). Beyond this the LLM is likely broken.
const maxConsecutiveFailures = 5

type RunResult struct {
	FinalOutput string
	Error       error
	Steps       int
}

type RunCallback func(action, toolName, toolArgs, output string)

func (a *Agent) Run(input string, callback RunCallback) *RunResult {
	a.startRun(input)

	defer a.logGovernanceSummary()
	a.applyUserSubmitHooks()

	for !a.finished.Load() {
		if a.stepLimitReached() {
			break
		}
		if finished := a.runTurn(input, callback); finished {
			a.finished.Store(true)
		}
	}

	a.evaluateStop()
	return a.finishRun(callback)
}

func (a *Agent) startRun(input string) {
	a.Reset()
	a.ctxMgr.Add("user", input, "user")
}

func (a *Agent) applyUserSubmitHooks() {
	if a.gov == nil || a.gov.HookReg == nil {
		return
	}
	for _, r := range a.gov.HookReg.Evaluate(hooks.UserSubmit, "", false) {
		a.injectHint(r.Hint)
	}
}

func (a *Agent) logGovernanceSummary() {
	if a.gov != nil {
		a.gov.LogSummary(a.step)
	}
}

func (a *Agent) stepLimitReached() bool {
	if a.step < maxAgentSteps {
		return false
	}
	a.stopReason = hooks.StopCompleted
	a.lastText = ""
	a.finalText = ""
	return true
}

func (a *Agent) finishRun(callback RunCallback) *RunResult {
	if a.getCtx().Err() != nil || a.stopReason == hooks.StopInterrupted {
		return &RunResult{FinalOutput: messages.MsgInterrupted, Steps: a.step}
	}
	if a.stopReason == hooks.StopCompleted {
		output := a.finalText
		if output == "" {
			output = a.lastText
		}
		if output != "" {
			return &RunResult{FinalOutput: output, Steps: a.step}
		}
	}
	return a.synthesizeAndReturn(callback)
}

func (a *Agent) evaluateStop() {
	if a.gov != nil && a.gov.HookReg != nil {
		for _, r := range a.gov.HookReg.Evaluate(hooks.Stop, "", false) {
			if r.Stop != nil {
				a.stopReason = *r.Stop
			}
			if r.Hint != nil {
				a.injectHint(r.Hint)
			}
		}
	}
}

func (a *Agent) runTurn(input string, callback RunCallback) (finished bool) {
	msgCountBefore := a.ctxMgr.Len()

	quota := a.prepareTurn(input)
	defer a.ctxMgr.SetHints("")

	if a.interruptedBeforeReasoning(callback) {
		return true
	}

	reasoning := a.Reason(input)
	if a.retryAfterInterruptedReasoning(reasoning, msgCountBefore) {
		return false
	}
	if a.stopReason == hooks.StopInterrupted {
		return true
	}

	calls := reasoning.ToolCalls
	if len(calls) > 0 {
		if a.handleToolCalls(calls, reasoning, &quota, callback) {
			return true
		}
		return false
	}

	return a.handleText(reasoning, callback)
}
