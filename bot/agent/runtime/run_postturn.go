package runtime

import (
	"nekocode/bot/agent/ledger"
	"nekocode/bot/hooks"
)

func (a *Agent) applyPostTurnHooks(reasoning *ReasoningResult, recordable bool, callback RunCallback) bool {
	if a.gov == nil || a.gov.HookReg == nil {
		return false
	}
	if reasoning.GarbledToolCall {
		a.gov.HookReg.Inc(hooks.StoreRespGarbled)
	}

	for _, r := range a.gov.HookReg.Evaluate(hooks.PostTurn, "", false) {
		if r.Stop != nil {
			a.stopReason = *r.Stop
			a.lastText = reasoning.ActionInput
			if recordable {
				a.finalText = reasoning.ActionInput
			}
			return true
		}
		if r.BlockFinal != nil {
			return a.applyFinalPolicyBlock(reasoning, r.BlockFinal.Reason)
		}
		if r.RequireTool != nil {
			reason := r.RequireTool.Reason
			if r.RequireTool.Tool != "" {
				reason = PolicyRequireTool(r.RequireTool.Tool, reason)
			}
			return a.applyFinalPolicyBlock(reasoning, reason)
		}
		if r.Hint != nil {
			return a.applyPostTurnHint(reasoning, r.Hint, recordable, callback)
		}
	}
	return false
}

func (a *Agent) applyPostTurnHint(reasoning *ReasoningResult, hint *hooks.Hint, recordable bool, callback RunCallback) bool {
	a.consecutiveHints++
	if a.consecutiveHints >= maxConsecutiveHints {
		a.step++
		a.stopReason = hooks.StopCompleted
		if reasoning.IsError || reasoning.GarbledToolCall {
			a.lastText = ""
			a.finalText = ""
		} else {
			a.lastText = reasoning.ActionInput
		}
		return true
	}
	if recordable && a.consecutiveHints == 1 {
		a.finalText = reasoning.ActionInput
	}
	if recordable {
		a.ctxMgr.AddAssistantResponse(reasoning.ActionInput, a.lastReason)
		if callback != nil {
			callback(reasoning.Action.String(), "", "", reasoning.ActionInput)
		}
	}
	a.lastText = reasoning.ActionInput
	a.injectHint(hint)
	a.step++
	return true
}

func (a *Agent) applyFinalCheck(reasoning *ReasoningResult) bool {
	if a.gov == nil || a.gov.Ledger == nil {
		return false
	}
	issues := a.gov.CheckFinalAnswer(reasoning.ActionInput)
	if len(issues) == 0 {
		return false
	}
	msg := ledger.FormatIssues(issues)
	a.lastText = reasoning.ActionInput

	if a.gov.Gate == nil {
		a.gov.Gate = NewResponseGate()
	}
	retry, hint := a.gov.Gate.TryRetry(msg)
	if !retry {
		return false
	}
	a.injectHint(&hooks.Hint{Type: "final_check", Severity: "critical", Content: hint})
	a.step++
	return true
}

func (a *Agent) applyFinalPolicyBlock(reasoning *ReasoningResult, reason string) bool {
	if reason == "" {
		reason = PolicyBlockFinal
	}
	a.lastText = reasoning.ActionInput

	if a.gov == nil || a.gov.Gate == nil {
		a.gov.Gate = NewResponseGate()
	}
	retry, hint := a.gov.Gate.TryRetry(reason)
	if !retry {
		return false
	}
	a.injectHint(&hooks.Hint{Type: "policy_block", Severity: "critical", Content: hint})
	a.step++
	return true
}
