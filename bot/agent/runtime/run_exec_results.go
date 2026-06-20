package runtime

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/llm/types"
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

func (a *Agent) mergeToolResults(calls []tools.ToolCallItem, blocked map[int]string, execResults []tools.ToolCallResult) []tools.ToolCallResult {
	results := make([]tools.ToolCallResult, len(calls))
	execIdx := 0
	for i := range calls {
		if msg, ok := blocked[i]; ok {
			results[i] = tools.ToolCallResult{ID: calls[i].ID, Name: calls[i].Name, Output: msg}
			if a.gov != nil {
				a.gov.RecordToolCall(ToolCallInfo{Name: calls[i].Name, Args: calls[i].Args}, true, msg)
			}
			continue
		}
		results[i] = execResults[execIdx]
		execIdx++
	}
	return results
}

func (a *Agent) recordExecutedToolCalls(calls []tools.ToolCallItem, blocked map[int]string, results []tools.ToolCallResult) {
	if a.gov == nil {
		return
	}
	for i, tc := range calls {
		if _, skip := blocked[i]; skip {
			continue
		}
		a.gov.RecordToolCall(ToolCallInfo{
			Name:   tc.Name,
			Args:   tc.Args,
			Output: results[i].Output,
			Error:  results[i].Error,
		}, false, "")
	}
}

func emitToolResultCallbacks(calls []tools.ToolCallItem, results []tools.ToolCallResult, callback RunCallback) []types.Message {
	msgs := make([]types.Message, len(results))
	for i, r := range results {
		content := r.EffectiveOutput()
		msgs[i] = types.Message{Content: content, ToolCallID: r.ID}
		if callback != nil {
			callback("execute_tool", r.Name, tools.FormatArgs(calls[i].Args), content)
		}
	}
	return msgs
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
