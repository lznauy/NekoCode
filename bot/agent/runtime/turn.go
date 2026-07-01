package runtime

import (
	"nekocode/bot/agent/runtime/toolrun"
	"nekocode/bot/hooks"
	aggov "nekocode/bot/policy"
	"nekocode/bot/policy/budget"
	"nekocode/bot/tools"
)

type turnRunner struct {
	agent *Agent
}

const policyBlockFinal = "final answer blocked by policy"

func newTurnRunner(agent *Agent) *turnRunner {
	return &turnRunner{agent: agent}
}

func (r *turnRunner) prepareTurn(input string) budget.ToolQuota {
	a := r.agent
	a.deps.ctxMgr.AutoCompactIfNeeded()
	quota := budget.ComputeQuota(a.deps.ctxMgr.TokenUsage())
	r.applyPreTurnHooks(input, quota)
	return quota
}

func (r *turnRunner) applyPreTurnHooks(input string, quota budget.ToolQuota) {
	a := r.agent
	if a.deps.gov == nil || a.deps.gov.HookReg == nil {
		a.applyTurnHints(nil)
		return
	}
	a.deps.gov.ResetTurnBetween(input, aggov.QuotaData{
		MaxSlots: quota.MaxSlots,
		Used:     quota.Used,
	})
	a.deps.gov.HookReg.Flag(hooks.StoreTasksAllDone, a.deps.ctxMgr.AllTasksDone())
	a.deps.gov.HookReg.Flag(hooks.StoreHasTasks, a.deps.ctxMgr.HasTasks())

	var hints []hooks.Hint
	for _, r := range a.deps.gov.HookReg.Evaluate(hooks.PreTurn, "", false) {
		if r.Hint != nil {
			hints = append(hints, *r.Hint)
		}
	}
	a.applyTurnHints(hints)
}

func (r *turnRunner) interruptedBeforeReasoning(callback RunCallback) bool {
	a := r.agent
	a.drainSteering()
	if a.getCtx().Err() == nil {
		return false
	}
	a.run.stopReason = hooks.StopInterrupted
	a.run.lastText = msgInterrupted
	if callback != nil {
		callback("chat", "", "", msgInterrupted)
	}
	return true
}

func (r *turnRunner) retryAfterInterruptedReasoning(reasoning *ReasoningResult, msgCountBefore int) bool {
	a := r.agent
	if !reasoning.Interrupted {
		return false
	}
	if a.life.finished.Load() {
		a.run.stopReason = hooks.StopInterrupted
		return false
	}
	// Count interrupted responses toward the step limit to prevent
	// unbounded loops when the LLM repeatedly produces interrupted output.
	a.run.step++
	a.deps.ctxMgr.TruncateTo(msgCountBefore)
	a.drainSteering()
	return true
}

func (r *turnRunner) handleToolCalls(calls []tools.ToolCallItem, reasoning *ReasoningResult, quota *budget.ToolQuota, callback RunCallback) bool {
	a := r.agent
	a.run.consecutiveHints = 0
	a.run.consecutiveFailures = 0
	a.run.gate.Reset()
	return a.toolRunner.ExecuteAndFeedback(calls, reasoning.TextContent, quota, toolrun.Callback(callback))
}

func (r *turnRunner) handleText(reasoning *ReasoningResult, callback RunCallback) (finished bool) {
	a := r.agent
	if reasoning.IsError {
		a.run.consecutiveFailures++
		if a.run.consecutiveFailures >= maxConsecutiveFailures {
			a.run.step++
			a.run.stopReason = hooks.StopCompleted
			a.run.lastText = ""
			return true
		}
	} else {
		a.run.consecutiveFailures = 0
	}

	recordable := isRecordableText(reasoning)
	if r.applyPostTurnHooks(reasoning, recordable, callback) {
		return a.run.stopReason == hooks.StopCompleted || a.run.stopReason == hooks.StopInterrupted || a.run.stopReason == hooks.StopFormatError
	}

	r.completeWithText(reasoning, recordable, callback)
	return true
}

func isRecordableText(reasoning *ReasoningResult) bool {
	return !reasoning.IsError && !reasoning.GarbledToolCall && reasoning.Action == ActionChat
}

func (r *turnRunner) completeWithText(reasoning *ReasoningResult, recordable bool, callback RunCallback) {
	a := r.agent
	a.run.stopReason = hooks.StopCompleted
	a.run.step++
	r.recordReasoningText(reasoning, recordable)
	if callback != nil {
		callback(reasoning.Action.String(), "", "", reasoning.ActionInput)
	}
}

func (r *turnRunner) recordReasoningText(reasoning *ReasoningResult, recordable bool) {
	a := r.agent
	a.run.lastText = reasoning.ActionInput
	if recordable {
		a.deps.ctxMgr.AddAssistantResponse(reasoning.ActionInput, a.stream.lastReason)
		a.run.finalText = reasoning.ActionInput
	}
}

func (r *turnRunner) applyPostTurnHooks(reasoning *ReasoningResult, recordable bool, callback RunCallback) bool {
	a := r.agent
	if a.deps.gov == nil || a.deps.gov.HookReg == nil {
		return false
	}
	if reasoning.GarbledToolCall {
		a.deps.gov.HookReg.Inc(hooks.StoreRespGarbled)
	}
	// Expose the final-answer text to PostTurn hooks (esp. final_check).
	// Only recordable text (non-error, non-garbled chat) is governed.
	if recordable {
		a.deps.gov.HookReg.SetStr(hooks.StoreFinalAnswerText, reasoning.ActionInput)
	} else {
		a.deps.gov.HookReg.SetStr(hooks.StoreFinalAnswerText, "")
	}

	for _, result := range a.deps.gov.HookReg.Evaluate(hooks.PostTurn, "", false) {
		if result.Stop != nil {
			a.run.stopReason = *result.Stop
			r.recordReasoningText(reasoning, recordable)
			return true
		}
		if result.BlockFinal != nil {
			return r.applyFinalPolicyBlock(reasoning, result.BlockFinal.Reason)
		}
		if result.RequireTool != nil {
			reason := policyRequireTool(result.RequireTool.Tool, result.RequireTool.Reason)
			return r.applyFinalPolicyBlock(reasoning, reason)
		}
		if result.Hint != nil {
			return r.applyPostTurnHint(reasoning, result.Hint, recordable, callback)
		}
	}
	return false
}

func (r *turnRunner) applyPostTurnHint(reasoning *ReasoningResult, hint *hooks.Hint, recordable bool, callback RunCallback) bool {
	a := r.agent
	a.run.consecutiveHints++
	if a.run.consecutiveHints >= maxConsecutiveHints {
		a.run.step++
		a.run.stopReason = hooks.StopCompleted
		if reasoning.IsError || reasoning.GarbledToolCall {
			a.run.lastText = ""
			a.run.finalText = ""
		} else {
			r.recordReasoningText(reasoning, recordable)
		}
		return true
	}
	r.recordReasoningText(reasoning, recordable)
	if recordable {
		if callback != nil {
			callback(reasoning.Action.String(), "", "", reasoning.ActionInput)
		}
	}
	a.injectHint(hint)
	a.run.step++
	return true
}

func (r *turnRunner) applyFinalPolicyBlock(reasoning *ReasoningResult, reason string) bool {
	a := r.agent
	if reason == "" {
		reason = policyBlockFinal
	}
	a.run.lastText = reasoning.ActionInput

	retry, hint := a.run.gate.TryRetry(reason)
	if !retry {
		return false
	}
	a.injectHint(&hooks.Hint{Type: "policy_block", Severity: "critical", Content: hint})
	a.run.step++
	return true
}

func policyRequireTool(tool, reason string) string {
	if tool != "" {
		return "必须先调用 " + tool + "：" + reason
	}
	return reason
}
