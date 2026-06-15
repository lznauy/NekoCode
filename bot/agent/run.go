package agent

import (
	"fmt"

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
	a.ctxMgr.Add("user", input)
	state := &stepState{Input: input}

	// UserSubmit hooks.
	if a.hookReg != nil {
		for _, r := range a.hookReg.Evaluate(hooks.UserSubmit, "", false) {
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
	if a.stopReason == hooks.StopCompleted && a.lastText != "" {
		return &RunResult{FinalOutput: a.lastText, Steps: a.step}
	}
	return a.synthesizeAndReturn(callback)
}

func (a *Agent) evaluateStop() {
	if a.hookReg != nil {
		for _, r := range a.hookReg.Evaluate(hooks.Stop, "", false) {
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
	if a.hookReg != nil {
		a.hookReg.ResetTurn()
		a.hookReg.Set(hooks.StoreQuotaReads, int64(max(0, state.quota.MaxSlots-state.quota.Used)))
		a.hookReg.Set(hooks.StoreExploreScore, int64(a.exploration.Score))
		tasksDone := int64(0)
		if a.ctxMgr.AllTasksDone() {
			tasksDone = 1
		}
		a.hookReg.Set(hooks.StoreTasksAllDone, tasksDone)
		hasTasks := int64(0)
		if a.ctxMgr.HasTasks() {
			hasTasks = 1
		}
		a.hookReg.Set(hooks.StoreHasTasks, hasTasks)
		a.hookReg.SetStr(hooks.StoreStepInput, state.Input)
		a.hookReg.Set(hooks.StoreStepInputLen, int64(len([]rune(state.Input))))

		var hints []hooks.Hint
		for _, r := range a.hookReg.Evaluate(hooks.PreTurn, "", false) {
			if r.Hint != nil {
				hints = append(hints, *r.Hint)
			}
		}
		a.ctxMgr.SetHints(hooks.FormatHints(hints))
	}

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
		var stopReason hooks.StopReason
		var shouldStop bool
		state, shouldStop, stopReason = a.executeAndFeedback(calls, reasoning, state, callback)
		if shouldStop {
			a.stopReason = stopReason
			return true
		}
		return false
	}

	return a.handleText(reasoning, state, callback)
}

func (a *Agent) handleText(reasoning *ReasoningResult, state *stepState, callback RunCallback) (finished bool) {
	// Track consecutive LLM failures.
	if reasoning.IsError {
		a.consecutiveFailures++
		if a.consecutiveFailures >= maxConsecutiveFailures {
			a.step++
			a.stopReason = hooks.StopCompleted
			a.lastText = fmt.Sprintf("[Agent stopped: %d consecutive LLM failures]", a.consecutiveFailures)
			return true
		}
	} else {
		a.consecutiveFailures = 0
	}

	if a.hookReg != nil {
		if reasoning.GarbledToolCall {
			a.hookReg.Inc(hooks.StoreRespGarbled)
		}

		// —— PostTurn hooks ——
		results := a.hookReg.Evaluate(hooks.PostTurn, "", false)
		for _, r := range results {
			if r.Stop != nil {
				a.stopReason = *r.Stop
				a.lastText = reasoning.ActionInput
				return true
			}
			if r.Hint != nil {
				a.consecutiveHints++
				if a.consecutiveHints >= maxConsecutiveHints {
					a.stopReason = hooks.StopCompleted
					a.lastText = reasoning.ActionInput
					return true
				}
				// Save this turn's text before re-injecting, so the
				// TUI stream accumulates it and handleDone can fall back.
				if reasoning.Action == ActionChat {
					a.ctxMgr.AddAssistantResponse(reasoning.ActionInput, a.lastReason)
					if callback != nil {
						callback(reasoning.Action.String(), "", "", reasoning.ActionInput)
					}
				}
				a.lastText = reasoning.ActionInput
				a.injectHint(r.Hint)
				a.step++
				return false
			}
		}
	}

	a.stopReason = hooks.StopCompleted
	a.step++
	a.lastText = reasoning.ActionInput
	if reasoning.Action == ActionChat {
		a.ctxMgr.AddAssistantResponse(reasoning.ActionInput, a.lastReason)
	}
	if callback != nil {
		callback(reasoning.Action.String(), "", "", reasoning.ActionInput)
	}
	return true
}

// injectHint adds a hook hint to the context as a system message.
func (a *Agent) injectHint(h *hooks.Hint) {
	if h != nil {
		a.ctxMgr.Add("system", "[Hook: "+h.Type+"] "+h.Content)
	}
}

// synthesizeAndReturn 额外调一次 LLM 生成总结，用于非正常结束的兜底输出。
func (a *Agent) synthesizeAndReturn(callback RunCallback) *RunResult {
	output := a.forceSynthesize()
	a.ctxMgr.AddAssistantResponse(output, "")
	if callback != nil {
		callback("chat", "", "", output)
	}
	return &RunResult{FinalOutput: output, Steps: a.step}
}

// drainSteering 清空 steering 通道中积压的用户中途消息。
func (a *Agent) drainSteering() {
	for {
		select {
		case msg := <-a.steering:
			a.ctxMgr.Add("user", msg)
		default:
			return
		}
	}
}
