package agent

import (
	"fmt"

	"nekocode/bot/ctxmgr"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/llm/types"

	"nekocode/bot/agent/subagent"

	"github.com/google/uuid"
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
	}
	// Generate previews for mutable tools (e.g. edit) before firing
	// tool_start callbacks so the TUI has diff content immediately.
	a.executor.PreparePreviews(allowed)
	for i, c := range calls {
		if callback != nil {
			action := "tool_start"
			if _, ok := blocked[i]; ok {
				action = "tool_blocked"
			}
			preview, _ := c.Args["_preview"].(string)
			callback(action, c.Name, tools.FormatArgs(c.Args), preview)
		}
	}

	// PreToolUse hooks (per-tool) — evaluate now, inject after tool results.
	var preToolHints []*hooks.Hint
	if a.hookReg != nil {
		for _, c := range allowed {
			for _, r := range a.hookReg.Evaluate(hooks.PreToolUse, c.Name, false) {
				if r.Hint != nil {
					preToolHints = append(preToolHints, r.Hint)
				}
			}
		}
	}

	// Execute allowed tools. PreviewFn bridges executor previews to TUI callbacks.
	// Sub-agent lifecycle: inject callbacks for task tool calls.
	type subSlotInfo struct {
		subID    string
		colorIdx int
	}
	var taskInfos []subSlotInfo
	for i, c := range allowed {
		if c.Name != "task" {
			continue
		}
		subType, _ := c.Args["type"].(string)
		if subType == "" {
			subType = "executor"
		}
		subID := uuid.New().String()
		colorIdx, ok := a.subSlotMgr.Acquire(subID, subType)
		if !ok {
			debug.Log("subSlotMgr: Acquire failed for %s (all slots full)", subType)
			continue
		}
		if callback != nil {
			callback("sub_agent_start", subType, subID, fmt.Sprint(colorIdx))
		}
		// Capture values for closures.
		sid := subID
		cid := colorIdx
		taskInfos = append(taskInfos, subSlotInfo{sid, cid})
		// Store the sub-callback in args — task tool Execute reads it.
		allowed[i].Args["_sub_callback"] = subagent.SubCallbackFn(func(action, toolName, toolArgs, output string) {
			if callback != nil {
				sidTag := fmt.Sprintf("%s:%d", sid, cid)
				switch action {
				case "sub_tool_start":
					callback(action, toolName, toolArgs, sidTag)
				case "sub_execute_tool":
					callback(action, toolName, sidTag, output)
				default:
					callback(action, toolName, toolArgs, output)
				}
			}
		})
	}
	// Defer cleanup: Release slots and send sub_agent_end after all task tools complete.
	defer func() {
		for _, ti := range taskInfos {
			a.subSlotMgr.Release(ti.subID)
			if callback != nil {
				callback("sub_agent_end", "", ti.subID, "")
			}
		}
	}()

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

	for i, tc := range calls {
		if _, blocked := blocked[i]; blocked {
			continue
		}
		a.exploration.Record(tc.Name)
		if a.hookReg != nil {
			if tc.Name == "read" || tc.Name == "grep" || tc.Name == "glob" || tc.Name == "list" {
				a.hookReg.Inc(hooks.StoreExploreCalls)
			}
			a.hookReg.Inc(hooks.StoreToolPrefix + tc.Name)
			a.hookReg.Inc(hooks.StoreTurnToolCalls)
			if tc.Name == "write" || tc.Name == "edit" || tc.Name == "bash" {
				a.hookReg.Flag(hooks.StoreFileModified, true)
				a.hookReg.Set(hooks.StoreHasEdits, 1)
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
		content := r.EffectiveOutput()
		msgs[i] = types.Message{Content: content, ToolCallID: r.ID}
		if callback != nil {
			callback("execute_tool", r.Name, tools.FormatArgs(calls[i].Args), content)
		}
	}

	// PostToolUse hooks (per-tool, skip blocked) — evaluate now, inject after tool results.
	var postToolHints []*hooks.Hint
	if a.hookReg != nil {
		for i, r := range results {
			if _, skip := blocked[i]; skip {
				continue
			}
			toolErr := r.Error != ""
			for _, hr := range a.hookReg.Evaluate(hooks.PostToolUse, calls[i].Name, toolErr) {
				if hr.Hint != nil {
					postToolHints = append(postToolHints, hr.Hint)
				}
			}
		}
	}

	// Add tool results to context.
	toolResults := make([]ctxmgr.ToolResultMsg, len(msgs))
	for i, m := range msgs {
		toolResults[i] = ctxmgr.ToolResultMsg{Message: m, ToolName: calls[i].Name}
	}
	a.ctxMgr.AddToolResultsBatch(toolResults)

	// Inject PreToolUse hints after tool results (must not break tool_calls sequence).
	for _, h := range preToolHints {
		a.injectHint(h)
	}

	// Inject PostToolUse hints after tool results (must not break tool_calls sequence).
	for _, h := range postToolHints {
		a.injectHint(h)
	}

	if a.hookReg != nil {
		for _, r := range a.hookReg.Evaluate(hooks.PostTool, "", false) {
			if r.Stop != nil {
				a.stopReason = *r.Stop
				// PostTool Stop: model never produced "chat" action with tool results.
				// Clear lastText to force synthesizeAndReturn so the TUI shows
				// a proper answer instead of previous turn's intermediate text.
				a.lastText = ""
				ns := *state
				return &ns, true, *r.Stop
			}
			a.injectHint(r.Hint)
		}
	}
	a.step++
	ns := *state
	return &ns, false, hooks.StopCompleted
}
