package agent

import (
	"fmt"
	"nekocode/bot/ctxmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/llm"
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
		writeAgentLog("quota: extended (extend %d/2)", state.quota.ExtendCount())
	}

	// Quota filter + tool_start callbacks.
	allowed := make([]tools.ToolCallItem, 0, len(calls))
	blocked := make(map[int]string)
	for i, c := range calls {
		if err := state.quota.ConsumeTool(c.Name); err != nil {
			blocked[i] = err.Error()
			writeAgentLog("quota: blocked %s — %v", c.Name, err)
		} else {
			allowed = append(allowed, c)
		}
		if callback != nil {
			action := "tool_start"
			if _, ok := blocked[i]; ok {
				action = "tool_blocked"
			}
			callback(action, c.Name, formatArgs(c.Args), "")
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


	// Track exploration + file modifications.
	for i, r := range results {
		if i < len(calls) {
			a.exploration.Record(calls[i].Name, extractFilePath(calls[i]))
			if r.Error == "" {
				switch calls[i].Name {
				case "write", "edit":
					state.filesModified = true
					state.needsVerification = true
				}
			}
		}
	}

	// Detect repeated tool calls (same name + same args across consecutive turns).
	currentSigs := make(map[string]bool, len(calls))
	for _, c := range calls {
		currentSigs[toolCallSignature(c)] = true
	}
	newCounts := make(map[string]int, len(currentSigs))
	for sig := range currentSigs {
		newCounts[sig] = state.consecutiveCalls[sig] + 1
	}
	state.consecutiveCalls = newCounts
	state.repeatedCallCount = 0

	state.repeatedCallName = ""
	for sig, count := range newCounts {
		if count > state.repeatedCallCount {
			state.repeatedCallCount = count
			state.repeatedCallName = sig
		}
	}

	// Build tool result messages.
	msgs := make([]llm.Message, len(results))
	for i, r := range results {
		content := r.Output
		if r.Error != "" {
			content = r.Error
		}
		msgs[i] = llm.Message{Content: content, ToolCallID: r.ID}
		if callback != nil {
			callback("execute_tool", r.Name, formatArgs(calls[i].Args), content)
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


	// Explore cascade detection.
	hasExplore := false
	for _, tc := range calls {
		if tc.Name == "task" {
			if t, _ := tc.Args["subagent_type"].(string); t == "explore" {
				hasExplore = true
				break
			}
		}
	}
	if hasExplore && !state.filesModified {
		state.exploreCascade++
	} else if !hasExplore {
		state.exploreCascade = 0
	}
	if state.exploreCascade >= 2 && !state.filesModified {
		writeAgentLog("exploreCascade: %d consecutive explores without progress", state.exploreCascade)
		state.exploreCascade = 0
	}

	a.step++
	return a.cloneState(state, calls), false, hooks.StopCompleted
}

func (a *Agent) cloneState(state *stepState, _ []tools.ToolCallItem) *stepState {
	ns := *state
	return &ns
}

// toolCallSignature returns a deterministic string key for a tool call (name + sorted args).
func toolCallSignature(tc tools.ToolCallItem) string {
	keys := make([]string, 0, len(tc.Args))
	for k := range tc.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(tc.Name)
	for _, k := range keys {
		sb.WriteString("|")
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(fmt.Sprint(tc.Args[k]))
	}
	return sb.String()
}
