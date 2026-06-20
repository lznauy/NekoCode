package runtime

import (
	"fmt"

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
				reason = "必须先调用 " + r.RequireTool.Tool + "：" + reason
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
			a.lastText = fmt.Sprintf("[Agent stopped: %d consecutive hints without progress]", a.consecutiveHints)
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
