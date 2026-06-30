package runtime

import "nekocode/bot/hooks"

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
