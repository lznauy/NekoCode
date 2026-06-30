package runtime

import (
	"nekocode/bot/agent/runtime/messages"
	"nekocode/bot/hooks"
)

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
			reason := requireToolReason(result.RequireTool.Tool, result.RequireTool.Reason)
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
		reason = messages.PolicyBlockFinal
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

func requireToolReason(tool, reason string) string {
	if tool == "" {
		return reason
	}
	return messages.PolicyRequireTool(tool, reason)
}
