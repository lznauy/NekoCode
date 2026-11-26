package llmstream

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// CollectToolCalls converts accumulated tool call deltas into ToolCallItems.
func (s *StreamResult) CollectToolCalls() ([]core.ToolCallItem, error) {
	if len(s.TcAccum) == 0 {
		return nil, nil
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
			return nil, fmt.Errorf("%w: missing tool call at index %d", protocol.ErrIncompleteResponse, idx)
		}
		var args map[string]any
		decoder := json.NewDecoder(strings.NewReader(acc.Args.String()))
		decoder.UseNumber()
		if err := decoder.Decode(&args); err != nil || args == nil {
			return nil, fmt.Errorf("%w: tool call %d arguments must be a JSON object", protocol.ErrIncompleteResponse, idx)
		}
		if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
			return nil, fmt.Errorf("%w: trailing data in tool call %d arguments", protocol.ErrIncompleteResponse, idx)
		}
		items = append(items, core.ToolCallItem{ID: acc.ID, Name: acc.Name, Args: args})
	}
	return items, nil
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
