package runtime

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"
)

func (a *Agent) executeAllowedTools(allowed []tools.ToolCallItem, callback RunCallback) []tools.ToolCallResult {
	if callback != nil {
		a.executor.SetPreviewFn(func(toolName string, _ map[string]any, preview string) {
			callback("tool_preview", toolName, "", preview)
		})
	} else {
		a.executor.SetPreviewFn(nil)
	}
	if len(allowed) == 0 {
		return nil
	}
	return a.executor.ExecuteBatch(a.getCtx(), allowed)
}

func (a *Agent) recordToolCalls(calls []tools.ToolCallItem, blocked map[int]string, results []tools.ToolCallResult) {
	if a.gov == nil {
		return
	}
	for i, tc := range calls {
		if msg, ok := blocked[i]; ok {
			a.gov.RecordToolCall(aggov.ToolCallInfo{Name: tc.Name, Args: tc.Args}, true, msg)
			continue
		}
		a.gov.RecordToolCall(aggov.ToolCallInfo{
			Name:   tc.Name,
			Args:   tc.Args,
			Output: results[i].Output,
			Error:  results[i].Error,
		}, false, "")
	}
}

func (a *Agent) evaluatePostToolUseHints(calls []tools.ToolCallItem, blocked map[int]string, results []tools.ToolCallResult) []*hooks.Hint {
	if a.gov == nil || a.gov.HookReg == nil {
		return nil
	}
	var hints []*hooks.Hint
	for i, r := range results {
		if _, skip := blocked[i]; skip {
			continue
		}
		toolErr := r.Error != ""
		for _, hr := range a.gov.HookReg.Evaluate(hooks.PostToolUse, calls[i].Name, toolErr, calls[i].Args) {
			if hr.Hint != nil {
				hints = append(hints, hr.Hint)
			}
		}
	}
	return hints
}

func (a *Agent) addToolResultsAndHints(calls []tools.ToolCallItem, msgs []types.Message, preToolHints, postToolHints []*hooks.Hint) {
	toolResults := make([]ctxmgr.ToolResultMsg, len(msgs))
	for i, m := range msgs {
		toolResults[i] = ctxmgr.ToolResultMsg{Message: m, ToolName: calls[i].Name}
	}
	a.ctxMgr.AddToolResultsBatch(toolResults)

	for _, h := range preToolHints {
		a.injectHint(h)
	}
	for _, h := range postToolHints {
		a.injectHint(h)
	}
}
