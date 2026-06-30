package toolflow

import (
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
)

type Callback func(action, toolName, toolArgs, output string)

func EmitStartCallbacks(calls []tools.ToolCallItem, blocked map[int]string, callback Callback) {
	if callback == nil {
		return
	}
	for i, c := range calls {
		action := "tool_start"
		if _, ok := blocked[i]; ok {
			action = "tool_blocked"
		}
		preview, _ := c.Args["_preview"].(string)
		callback(action, c.Name, tools.FormatArgs(c.Args), preview)
	}
}

func MergeResults(calls []tools.ToolCallItem, blocked map[int]string, execResults []tools.ToolCallResult) []tools.ToolCallResult {
	results := make([]tools.ToolCallResult, len(calls))
	execIdx := 0
	for i := range calls {
		if msg, ok := blocked[i]; ok {
			results[i] = tools.ToolCallResult{ID: calls[i].ID, Name: calls[i].Name, Error: msg}
			continue
		}
		results[i] = execResults[execIdx]
		execIdx++
	}
	return results
}

func EmitResultCallbacks(calls []tools.ToolCallItem, results []tools.ToolCallResult, callback Callback) []types.Message {
	msgs := make([]types.Message, len(results))
	for i, r := range results {
		content := r.EffectiveOutput()
		msgs[i] = types.Message{Content: content, ToolCallID: r.ID, IsError: r.Error != ""}
		if callback != nil {
			callback("execute_tool", r.Name, tools.FormatArgs(calls[i].Args), content)
		}
	}
	return msgs
}
