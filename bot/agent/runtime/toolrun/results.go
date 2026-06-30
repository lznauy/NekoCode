package toolrun

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"
)

func (r *Runner) executeAllowedTools(allowed []tools.ToolCallItem, callback Callback) []tools.ToolCallResult {
	executor := r.host.Executor()
	if callback != nil {
		executor.SetPreviewFn(func(toolName string, _ map[string]any, preview string) {
			callback("tool_preview", toolName, "", preview)
		})
	} else {
		executor.SetPreviewFn(nil)
	}
	if len(allowed) == 0 {
		return nil
	}
	return executor.ExecuteBatch(r.host.Context(), allowed)
}

func (r *Runner) recordToolCalls(calls []tools.ToolCallItem, blocked map[int]string, results []tools.ToolCallResult) {
	gov := r.host.Governance()
	if gov == nil {
		return
	}
	for i, tc := range calls {
		if msg, ok := blocked[i]; ok {
			gov.RecordToolCall(aggov.ToolCallInfo{Name: tc.Name, Args: tc.Args}, true, msg)
			continue
		}
		gov.RecordToolCall(aggov.ToolCallInfo{
			Name:   tc.Name,
			Args:   tc.Args,
			Output: results[i].Output,
			Error:  results[i].Error,
		}, false, "")
	}
}

func (r *Runner) evaluatePostToolUseHints(calls []tools.ToolCallItem, blocked map[int]string, results []tools.ToolCallResult) []*hooks.Hint {
	gov := r.host.Governance()
	if gov == nil || gov.HookReg == nil {
		return nil
	}
	var hints []*hooks.Hint
	for i, result := range results {
		if _, skip := blocked[i]; skip {
			continue
		}
		toolErr := result.Error != ""
		for _, hr := range gov.HookReg.Evaluate(hooks.PostToolUse, calls[i].Name, toolErr, calls[i].Args) {
			if hr.Hint != nil {
				hints = append(hints, hr.Hint)
			}
		}
	}
	return hints
}

func (r *Runner) addToolResultsAndHints(calls []tools.ToolCallItem, msgs []types.Message, preToolHints, postToolHints []*hooks.Hint) {
	toolResults := make([]ctxmgr.ToolResultMsg, len(msgs))
	for i, m := range msgs {
		toolResults[i] = ctxmgr.ToolResultMsg{Message: m, ToolName: calls[i].Name}
	}
	r.host.ContextManager().AddToolResultsBatch(toolResults)

	for _, h := range preToolHints {
		r.host.InjectHint(h)
	}
	for _, h := range postToolHints {
		r.host.InjectHint(h)
	}
}
