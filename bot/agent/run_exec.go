package agent

import (
	"nekocode/bot/ctxmgr"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/llm/types"
)

func (a *Agent) executeAndFeedback(calls []tools.ToolCallItem, reasoning *ReasoningResult, state *stepState, callback RunCallback) (*stepState, bool, hooks.StopReason) {
	if reasoning.TextContent != "" && callback != nil {
		callback("think", "", "", reasoning.TextContent)
	}

	// Quota filter + tool_start callbacks.
	allowed := make([]tools.ToolCallItem, 0, len(calls))
	blocked := make(map[int]string)
	for i, c := range calls {
		if err := state.quota.ConsumeTool(c.Name); err != nil {
			blocked[i] = err.Error()
			debug.Log("quota: blocked %s — %v", c.Name, err)
		} else {
			allowed = append(allowed, c)
		}
		if callback != nil {
			action := "tool_start"
			if _, ok := blocked[i]; ok {
				action = "tool_blocked"
			}
			preview, _ := c.Args["_preview"].(string)
			callback(action, c.Name, formatArgs(c.Args), preview)
		}
	}

	// PreToolUse hooks (per-tool).
	if a.hookReg != nil {
		for _, c := range allowed {
			for _, r := range a.hookReg.Evaluate(hooks.PreToolUse, c.Name, false) {
				if r.Hint != nil {
					a.ctxMgr.Add("user", "[System] "+r.Hint.Content)
				}
			}
		}
	}

	// Execute allowed tools. PreviewFn bridges executor previews to TUI callbacks.
	if callback != nil {
		a.executor.SetPreviewFn(func(toolName string, _ map[string]any, preview string) {
			callback("tool_preview", toolName, "", preview)
		})
	} else {
		a.executor.SetPreviewFn(nil)
	}
	var execResults []tools.ToolCallResult
	if len(allowed) > 0 {
		execResults = a.executor.ExecuteBatch(a.getCtx(), allowed)
	}
	results := make([]tools.ToolCallResult, len(calls))
	execIdx := 0
	for i := range calls {
		if msg, ok := blocked[i]; ok {
			results[i] = tools.ToolCallResult{ID: calls[i].ID, Name: calls[i].Name, Output: msg}
		} else {
			results[i] = execResults[execIdx]
			execIdx++
		}
	}

	if a.hookReg != nil {
		for i, tc := range calls {
			if _, blocked := blocked[i]; blocked {
				continue
			}
			a.exploration.Record(tc.Name)
			a.hookReg.Inc(hooks.StoreToolPrefix + tc.Name)
			if tc.Name == "write" || tc.Name == "edit" {
				a.hookReg.Flag(hooks.StoreFileModified, true)
			}
			if tc.Name == "task" {
				if t, _ := tc.Args["type"].(string); t == "researcher" {
					a.hookReg.Inc(hooks.StoreToolResearcher)
				}
			}
		}
	}

	// Build tool result messages.
	msgs := make([]types.Message, len(results))
	for i, r := range results {
		content := r.Output
		if r.Error != "" {
			content = r.Error
		}
		msgs[i] = types.Message{Content: content, ToolCallID: r.ID}
		if callback != nil {
			callback("execute_tool", r.Name, formatArgs(calls[i].Args), content)
		}
	}

	// PostToolUse hooks (per-tool).
	if a.hookReg != nil {
		for i := range results {
			if i < len(calls) {
				toolErr := results[i].Error != ""
				for _, hr := range a.hookReg.Evaluate(hooks.PostToolUse, calls[i].Name, toolErr) {
					if hr.Hint != nil {
						a.ctxMgr.Add("user", "[System] "+hr.Hint.Content)
					}
				}
			}
		}
	}

	// Add tool results to context.
	toolResults := make([]ctxmgr.ToolResultMsg, len(msgs))
	for i, m := range msgs {
		name := ""
		if i < len(calls) {
			name = calls[i].Name
		}
		toolResults[i] = ctxmgr.ToolResultMsg{Message: m, ToolName: name}
	}
	a.ctxMgr.AddToolResultsBatch(toolResults)

	if a.hookReg != nil {
		for _, r := range a.hookReg.Evaluate(hooks.PostTool, "", false) {
			if r.Stop != nil {
				a.stopReason = *r.Stop
				return a.cloneState(state, calls), true, *r.Stop
			}
			if r.Hint != nil {
				a.ctxMgr.Add("user", "[System] "+r.Hint.Content)
			}
		}
	}
	a.step++
	return a.cloneState(state, calls), false, hooks.StopCompleted
}

func (a *Agent) cloneState(state *stepState, _ []tools.ToolCallItem) *stepState {
	ns := *state
	return &ns
}
