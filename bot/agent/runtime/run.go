package runtime

import (
	"nekocode/bot/agent/budget"
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

// maxFinalCheckHints limits governance retries so a model that ignores final
// answer checks cannot loop forever.
const maxFinalCheckHints = 2

type RunResult struct {
	FinalOutput string
	Error       error
	Steps       int
}

type stepState struct {
	Input string
	quota budget.ToolQuota
}

type RunCallback func(action, toolName, toolArgs, output string)

func (a *Agent) Run(input string, callback RunCallback) *RunResult {
	a.Reset()
	a.ctxMgr.Add("user", input, "user")
	state := &stepState{Input: input}

	// Emit governance summary to debug log on completion.
	defer a.gov.LogSummary(a.step)

	// UserSubmit hooks.
	if a.gov != nil && a.gov.HookReg != nil {
		for _, r := range a.gov.HookReg.Evaluate(hooks.UserSubmit, "", false) {
			a.injectHint(r.Hint)
		}
	}

	for !a.finished.Load() {
		if a.step >= maxAgentSteps {
			a.stopReason = hooks.StopCompleted
			a.lastText = "[Agent stopped: reached maximum step limit]"
			break
		}
		if finished := a.runTurn(state, callback); finished {
			a.finished.Store(true)
		}
	}

	a.evaluateStop()

	if a.getCtx().Err() != nil || a.stopReason == hooks.StopInterrupted {
		return &RunResult{FinalOutput: "Interrupted", Steps: a.step}
	}
	if a.stopReason == hooks.StopCompleted {
		output := a.finalText
		if output == "" {
			output = a.lastText
		}
		// 过滤系统内部消息：maxAgentSteps 耗尽时 lastText 可能为
		// "[Agent stopped: ...]"，不应展示给用户。
		if output != "" && !isSystemMessage(output) {
			return &RunResult{FinalOutput: output, Steps: a.step}
		}
	}
	return a.synthesizeAndReturn(callback)
}

// logGovernanceSummary is defer-called. Deleted in Phase 3 (now in govManager).

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

func (a *Agent) runTurn(state *stepState, callback RunCallback) (finished bool) {
	msgCountBefore := a.ctxMgr.Len()

	a.ctxMgr.AutoCompactIfNeeded()
	state.quota = budget.ComputeQuota(a.ctxMgr.TokenUsage())

	// —— PreTurn hooks ——
	if a.gov != nil && a.gov.HookReg != nil {
		a.gov.ResetTurnBetween(state.Input, ToolQuotaData{
			MaxSlots: state.quota.MaxSlots,
			Used:     state.quota.Used,
		})

		tasksDone := int64(0)
		if a.ctxMgr.AllTasksDone() {
			tasksDone = 1
		}
		hasTasks := int64(0)
		if a.ctxMgr.HasTasks() {
			hasTasks = 1
		}
		a.gov.HookReg.Set(hooks.StoreTasksAllDone, tasksDone)
		a.gov.HookReg.Set(hooks.StoreHasTasks, hasTasks)

		var hints []hooks.Hint
		for _, r := range a.gov.HookReg.Evaluate(hooks.PreTurn, "", false) {
			if r.Hint != nil {
				hints = append(hints, *r.Hint)
			}
		}
		a.applyTurnHints(hints)
	} else {
		a.applyTurnHints(nil)
	}
	defer a.ctxMgr.SetHints("")

	a.drainSteering()
	if a.getCtx().Err() != nil {
		a.stopReason = hooks.StopInterrupted
		a.lastText = "Interrupted"
		if callback != nil {
			callback("chat", "", "", "Interrupted")
		}
		return true
	}

	reasoning := a.Reason(state)

	if reasoning.Interrupted {
		if a.finished.Load() {
			a.stopReason = hooks.StopInterrupted
			return true
		}
		// Count interrupted responses toward the step limit to prevent
		// unbounded loops when the LLM repeatedly produces interrupted output.
		a.step++
		a.ctxMgr.TruncateTo(msgCountBefore)
		a.drainSteering()
		return false
	}

	calls := reasoning.ToolCalls
	if len(calls) > 0 {
		// Tool calls mean the agent is making progress — reset hint counter.
		a.consecutiveHints = 0
		a.consecutiveFailures = 0
		if a.gov != nil && a.gov.Gate != nil {
			a.gov.Gate.Reset()
		}
		var stopReason hooks.StopReason
		var shouldStop bool
		shouldStop, stopReason = a.executeAndFeedback(calls, reasoning, state, callback)
		if shouldStop {
			a.stopReason = stopReason
			return true
		}
		return false
	}

	return a.handleText(reasoning, state, callback)
}
