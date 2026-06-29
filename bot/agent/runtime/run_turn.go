package runtime

import (
	"nekocode/bot/agent/budget"
	aggov "nekocode/bot/agent/governance"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
)

type stepState struct {
	Input string
	quota budget.ToolQuota
}

func (a *Agent) prepareTurn(state *stepState) {
	a.ctxMgr.AutoCompactIfNeeded()
	state.quota = budget.ComputeQuota(a.ctxMgr.TokenUsage())
	a.applyPreTurnHooks(state)
}

func (a *Agent) applyPreTurnHooks(state *stepState) {
	if a.gov == nil || a.gov.HookReg == nil {
		a.applyTurnHints(nil)
		return
	}
	a.gov.ResetTurnBetween(state.Input, aggov.QuotaData{
		MaxSlots: state.quota.MaxSlots,
		Used:     state.quota.Used,
	})
	a.gov.HookReg.Set(hooks.StoreTasksAllDone, boolStore(a.ctxMgr.AllTasksDone()))
	a.gov.HookReg.Set(hooks.StoreHasTasks, boolStore(a.ctxMgr.HasTasks()))

	var hints []hooks.Hint
	for _, r := range a.gov.HookReg.Evaluate(hooks.PreTurn, "", false) {
		if r.Hint != nil {
			hints = append(hints, *r.Hint)
		}
	}
	a.applyTurnHints(hints)
}

func boolStore(ok bool) int64 {
	if ok {
		return 1
	}
	return 0
}

func (a *Agent) interruptedBeforeReasoning(callback RunCallback) bool {
	a.drainSteering()
	if a.getCtx().Err() == nil {
		return false
	}
	a.stopReason = hooks.StopInterrupted
	a.lastText = MsgInterrupted
	if callback != nil {
		callback("chat", "", "", MsgInterrupted)
	}
	return true
}

func (a *Agent) retryAfterInterruptedReasoning(reasoning *ReasoningResult, msgCountBefore int) bool {
	if !reasoning.Interrupted {
		return false
	}
	if a.finished.Load() {
		a.stopReason = hooks.StopInterrupted
		return false
	}
	// Count interrupted responses toward the step limit to prevent
	// unbounded loops when the LLM repeatedly produces interrupted output.
	a.step++
	a.ctxMgr.TruncateTo(msgCountBefore)
	a.drainSteering()
	return true
}

func (a *Agent) handleToolCalls(calls []tools.ToolCallItem, reasoning *ReasoningResult, state *stepState, callback RunCallback) (bool, hooks.StopReason) {
	a.consecutiveHints = 0
	a.consecutiveFailures = 0
	a.gate.Reset()
	return a.executeAndFeedback(calls, reasoning, state, callback)
}

func (a *Agent) handleText(reasoning *ReasoningResult, callback RunCallback) (finished bool) {
	if reasoning.IsError {
		a.consecutiveFailures++
		if a.consecutiveFailures >= maxConsecutiveFailures {
			a.step++
			a.stopReason = hooks.StopCompleted
			a.lastText = ""
			return true
		}
	} else {
		a.consecutiveFailures = 0
	}

	recordable := isRecordableText(reasoning)
	if a.applyPostTurnHooks(reasoning, recordable, callback) {
		return a.stopReason == hooks.StopCompleted || a.stopReason == hooks.StopInterrupted || a.stopReason == hooks.StopFormatError
	}

	a.completeWithText(reasoning, recordable, callback)
	return true
}

func isRecordableText(reasoning *ReasoningResult) bool {
	return !reasoning.IsError && !reasoning.GarbledToolCall && reasoning.Action == ActionChat
}

func (a *Agent) completeWithText(reasoning *ReasoningResult, recordable bool, callback RunCallback) {
	a.stopReason = hooks.StopCompleted
	a.step++
	a.lastText = reasoning.ActionInput
	if recordable {
		a.ctxMgr.AddAssistantResponse(reasoning.ActionInput, a.lastReason)
		a.finalText = reasoning.ActionInput
	}
	if callback != nil {
		callback(reasoning.Action.String(), "", "", reasoning.ActionInput)
	}
}
