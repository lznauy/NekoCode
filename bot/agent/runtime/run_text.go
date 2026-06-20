package runtime

import (
	"fmt"

	"nekocode/bot/hooks"
)

func (a *Agent) handleText(reasoning *ReasoningResult, state *stepState, callback RunCallback) (finished bool) {
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

	recordable := isRecordableText(reasoning)
	if a.applyPostTurnHooks(reasoning, recordable, callback) {
		return a.stopReason == hooks.StopCompleted || a.stopReason == hooks.StopInterrupted || a.stopReason == hooks.StopFormatError
	}

	if recordable {
		if blocked := a.applyFinalCheck(reasoning); blocked {
			return false
		}
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
