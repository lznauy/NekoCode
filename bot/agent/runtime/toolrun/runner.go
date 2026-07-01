package toolrun

import (
	"context"

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
	SubSlots() *SlotManager
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

func policyRequireTool(tool, reason string) string {
	if tool != "" {
		return "必须先调用 " + tool + "：" + reason
	}
	return reason
}

func (r *Runner) ExecuteAndFeedback(calls []tools.ToolCallItem, textContent string, quota *budget.ToolQuota, callback Callback) bool {
	if textContent != "" && callback != nil {
		callback("think", "", "", textContent)
	}

	filtered := r.FilterToolCalls(calls, quota)
	r.host.Executor().PreparePreviews(filtered.Allowed)
	emitStartCallbacks(calls, filtered.Blocked, callback)

	cleanupSubagents := r.prepareSubagentCallbacks(filtered.Allowed, callback)
	defer cleanupSubagents()

	execResults := r.executeAllowedTools(filtered.Allowed, callback)
	results := mergeResults(calls, filtered.Blocked, execResults)
	r.recordToolCalls(calls, filtered.Blocked, results)

	msgs := emitResultCallbacks(calls, results, callback)
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
				Content:  policyRequireTool(result.RequireTool.Tool, result.RequireTool.Reason),
			})
		}
		if result.BlockFinal != nil {
			r.host.InjectHint(&hooks.Hint{Type: "block_final", Severity: "critical", Content: result.BlockFinal.Reason})
		}
		r.host.InjectHint(result.Hint)
	}
	return false
}
