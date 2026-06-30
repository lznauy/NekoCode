package toolrun

import (
	"context"

	"nekocode/bot/agent/runtime/messages"
	"nekocode/bot/agent/runtime/subagents"
	"nekocode/bot/agent/runtime/toolflow"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	aggov "nekocode/bot/policy"
	"nekocode/bot/policy/budget"
	"nekocode/bot/tools"
)

type Callback func(action, toolName, toolArgs, output string)

type Host interface {
	Context() context.Context
	ContextManager() *ctxmgr.Manager
	Executor() *tools.Executor
	Governance() *aggov.Manager
	SubSlots() *subagents.SlotManager
	InjectHint(*hooks.Hint)
	IncStep()
	StopPostTool(hooks.StopReason)
}

type Runner struct {
	host Host
}

func New(host Host) *Runner {
	return &Runner{host: host}
}

func (r *Runner) ExecuteAndFeedback(calls []tools.ToolCallItem, textContent string, quota *budget.ToolQuota, callback Callback) bool {
	if textContent != "" && callback != nil {
		callback("think", "", "", textContent)
	}

	filtered := r.FilterToolCalls(calls, quota)
	r.host.Executor().PreparePreviews(filtered.Allowed)
	toolflow.EmitStartCallbacks(calls, filtered.Blocked, toolflow.Callback(callback))

	cleanupSubagents := r.prepareSubagentCallbacks(filtered.Allowed, callback)
	defer cleanupSubagents()

	execResults := r.executeAllowedTools(filtered.Allowed, callback)
	results := toolflow.MergeResults(calls, filtered.Blocked, execResults)
	r.recordToolCalls(calls, filtered.Blocked, results)

	msgs := toolflow.EmitResultCallbacks(calls, results, toolflow.Callback(callback))
	postToolHints := r.evaluatePostToolUseHints(calls, filtered.Blocked, results)
	r.addToolResultsAndHints(calls, msgs, filtered.PreToolHints, postToolHints)

	if r.ApplyPostToolHooks() {
		return true
	}
	r.host.IncStep()
	return false
}

func (r *Runner) ApplyPostToolHooks() bool {
	gov := r.host.Governance()
	if gov == nil || gov.HookReg == nil {
		return false
	}
	for _, result := range gov.HookReg.Evaluate(hooks.PostTool, "", false) {
		if result.Stop != nil {
			r.host.StopPostTool(*result.Stop)
			return true
		}
		if result.RequireTool != nil {
			r.host.InjectHint(&hooks.Hint{
				Type:     "require_tool",
				Severity: "critical",
				Content:  requireToolReason(result.RequireTool.Tool, result.RequireTool.Reason),
			})
		}
		if result.BlockFinal != nil {
			r.host.InjectHint(&hooks.Hint{Type: "block_final", Severity: "critical", Content: result.BlockFinal.Reason})
		}
		r.host.InjectHint(result.Hint)
	}
	return false
}

func requireToolReason(tool, reason string) string {
	if tool == "" {
		return reason
	}
	return messages.PolicyRequireTool(tool, reason)
}
