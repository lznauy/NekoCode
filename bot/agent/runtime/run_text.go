package runtime

import (
	"fmt"
	"strings"

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

	// 过滤系统内部消息：LLM 可能将 guardrail 注入的 [System] 提示语原样返回，
	// 这类文本不应作为最终输出展示给用户。
	if isSystemMessage(reasoning.ActionInput) {
		a.lastText = ""
		a.step++
		return false
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

// isSystemMessage 检测文本是否为系统内部注入的提示语（如 guardrail 警告），
// 这类文本不应作为最终输出展示给用户。
func isSystemMessage(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasPrefix(t, "[System]") || strings.HasPrefix(t, "[Agent stopped:")
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
