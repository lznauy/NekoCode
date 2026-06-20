package runtime

import (
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
)

func (a *Agent) executeAndFeedback(calls []tools.ToolCallItem, reasoning *ReasoningResult, state *stepState, callback RunCallback) (bool, hooks.StopReason) {
	if reasoning.TextContent != "" && callback != nil {
		callback("think", "", "", reasoning.TextContent)
	}

	filtered := a.filterToolCalls(calls, state)
	a.executor.PreparePreviews(filtered.allowed)
	emitToolStartCallbacks(calls, filtered.blocked, callback)

	cleanupSubagents := a.prepareSubagentCallbacks(filtered.allowed, callback)
	defer cleanupSubagents()

	execResults := a.executeAllowedTools(filtered.allowed, callback)
	results := a.mergeToolResults(calls, filtered.blocked, execResults)
	a.recordExecutedToolCalls(calls, filtered.blocked, results)

	msgs := emitToolResultCallbacks(calls, results, callback)
	postToolHints := a.evaluatePostToolUseHints(calls, filtered.blocked, results)
	a.addToolResultsAndHints(calls, msgs, filtered.preToolHints, postToolHints)

	if shouldStop, stopReason := a.applyPostToolHooks(); shouldStop {
		return true, stopReason
	}
	a.step++
	return false, hooks.StopCompleted
}
