package llmstream

import (
	"encoding/json"
	"sort"

	"nekocode/bot/llm/types"
	"nekocode/bot/tools/core"
)

// CollectToolCalls converts accumulated tool call deltas into ToolCallItems.
func (s *StreamResult) CollectToolCalls() []core.ToolCallItem {
	if len(s.TcAccum) == 0 {
		return nil
	}
	indices := make([]int, 0, len(s.TcAccum))
	for idx := range s.TcAccum {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var items []core.ToolCallItem
	for _, idx := range indices {
		acc := s.TcAccum[idx]
		if acc == nil {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(acc.Args.String()), &args); err != nil {
			continue
		}
		items = append(items, core.ToolCallItem{ID: acc.ID, Name: acc.Name, Args: args})
	}
	return items
}

// ToLLMToolCalls converts ToolCallItems to the LLM types.ToolCall format.
func ToLLMToolCalls(calls []core.ToolCallItem) []types.ToolCall {
	out := make([]types.ToolCall, len(calls))
	for i, c := range calls {
		args, _ := json.Marshal(c.Args)
		out[i] = types.ToolCall{
			ID:       c.ID,
			Type:     "function",
			Function: types.FunctionCall{Name: c.Name, Arguments: string(args)},
		}
	}
	return out
}
