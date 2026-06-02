package agent

import (
	"nekocode/bot/debug"
	"fmt"
	"nekocode/bot/ctxmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/llm/types"
	"sort"
	"strings"
)

func (a *Agent) executeAndFeedback(calls []tools.ToolCallItem, reasoning *ReasoningResult, state *stepState, callback RunCallback) (*stepState, bool, hooks.StopReason) {
	if reasoning.TextContent != "" && callback != nil {
		callback("think", "", "", reasoning.TextContent)
	}

	// Quota extension request.
	if state.quota.TryExtend(reasoning.TextContent) {
		state.quota.UsedReads = 0
		state.quota.UsedGreps = 0
		a.exploration.RecordPenalty(30, "quota_extend")
		debug.Log("quota: extended (extend %d/2)", state.quota.ExtendCount())
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
			// Trigger DangerLevel to populate _preview before callback.
			if t, err := a.toolRegistry.Get(c.Name); err == nil {
				t.DangerLevel(c.Args)
			}
			preview, _ := c.Args["_preview"].(string)
			callback(action, c.Name, formatArgs(c.Args), preview)
		}
	}

	// Declarative PreToolUse hooks.
	if a.declHooks != nil {
		for _, c := range allowed {
			hints := a.declHooks.PreToolUse(c.Name)
			for _, h := range hints {
				a.ctxMgr.Add("user", "[System] "+h.Content)
			}
		}
	}

	// Execute allowed tools, merge blocked results.
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


		if a.hookMgr != nil {
		// Track exploration + emit hook events.
		for i, r := range results {
			if i < len(calls) {
				tc := calls[i]
				a.exploration.Record(tc.Name, extractFilePath(tc))
				a.hookMgr.Counter(hooks.KeyToolPrefix + tc.Name)
				if r.Error == "" && (tc.Name == "write" || tc.Name == "edit") {
					a.hookMgr.Flag(hooks.KeyFileModified, true)
				}
				if tc.Name == "task" {
					if t, _ := tc.Args["type"].(string); t == "researcher" {
						a.hookMgr.Turn(hooks.KeyToolTaskResearcher)
					}
				}
			}
		}
		a.hookMgr.Value(hooks.KeyToolSig, toolCallSignatureBatch(calls))
	
		}
	// Build tool result messages.
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

	// Declarative PostToolUse hooks.
	if a.declHooks != nil {
		for i, r := range results {
			if i < len(calls) {
				hints := a.declHooks.PostToolUse(calls[i].Name, r.Error == "")
				for _, h := range hints {
					a.ctxMgr.Add("user", "[System] "+h.Content)
				}
			}
		}
	}

	// Add tool results to context (always use batch, single result is just batch of 1).
	toolResults := make([]ctxmgr.ToolResultMsg, len(msgs))
	for i, m := range msgs {
		name := ""
		if i < len(calls) {
			name = calls[i].Name
		}
		toolResults[i] = ctxmgr.ToolResultMsg{Message: m, ToolName: name}
	}
	a.ctxMgr.AddToolResultsBatch(toolResults)


	if a.hookMgr != nil {
		// —— PostTool hooks ——
		for _, r := range a.hookMgr.Evaluate(hooks.PointPostTool) {
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

// toolCallSignature returns a deterministic string key for a tool call (name + sorted args).
func toolCallSignatureBatch(calls []tools.ToolCallItem) string {
	if len(calls) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, tc := range calls {
		keys := make([]string, 0, len(tc.Args))
		for k := range tc.Args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteString(tc.Name)
		for _, k := range keys {
			sb.WriteString("|")
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(fmt.Sprint(tc.Args[k]))
		}
		sb.WriteString(";")
	}
	return sb.String()
}
