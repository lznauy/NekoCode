package runtime

import (
	"nekocode/bot/agent/runtime/messages"
	"nekocode/bot/agent/runtime/toolflow"
	"nekocode/bot/hooks"
	"nekocode/bot/policy/budget"
	"nekocode/bot/tools"
)

func (a *Agent) executeAndFeedback(calls []tools.ToolCallItem, reasoning *ReasoningResult, quota *budget.ToolQuota, callback RunCallback) bool {
	if reasoning.TextContent != "" && callback != nil {
		callback("think", "", "", reasoning.TextContent)
	}

	filtered := a.filterToolCalls(calls, quota)
	a.executor.PreparePreviews(filtered.allowed)
	toolflow.EmitStartCallbacks(calls, filtered.blocked, toolflow.Callback(callback))

	cleanupSubagents := a.prepareSubagentCallbacks(filtered.allowed, callback)
	defer cleanupSubagents()

	execResults := a.executeAllowedTools(filtered.allowed, callback)
	results := toolflow.MergeResults(calls, filtered.blocked, execResults)
	a.recordToolCalls(calls, filtered.blocked, results)

	msgs := toolflow.EmitResultCallbacks(calls, results, toolflow.Callback(callback))
	postToolHints := a.evaluatePostToolUseHints(calls, filtered.blocked, results)
	a.addToolResultsAndHints(calls, msgs, filtered.preToolHints, postToolHints)

	if a.applyPostToolHooks() {
		return true
	}
	a.step++
	return false
}

func (a *Agent) applyPostToolHooks() bool {
	if a.gov == nil || a.gov.HookReg == nil {
		return false
	}
	for _, r := range a.gov.HookReg.Evaluate(hooks.PostTool, "", false) {
		if r.Stop != nil {
			a.stopReason = *r.Stop
			a.lastText = ""
			return true
		}
		if r.RequireTool != nil {
			reason := r.RequireTool.Reason
			if r.RequireTool.Tool != "" {
				reason = messages.PolicyRequireTool(r.RequireTool.Tool, reason)
			}
			a.injectHint(&hooks.Hint{Type: "require_tool", Severity: "critical", Content: reason})
		}
		if r.BlockFinal != nil {
			a.injectHint(&hooks.Hint{Type: "block_final", Severity: "critical", Content: r.BlockFinal.Reason})
		}
		a.injectHint(r.Hint)
	}
	return false
}
