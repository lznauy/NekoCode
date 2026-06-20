package contextmgr

import (
	"nekocode/llm/types"
)

func (m *Manager) Build(withTools bool) []types.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.ctx.BuildLayer0()
	out = append(out, m.ctx.BuildLayer0Mem()...)
	out = append(out, m.ctx.BuildLayer05()...)
	out = append(out, m.filterValidMessages(m.ctx.Messages)...)
	out = append(out, m.ctx.BuildLayer2()...)

	// Strip internal Source field; LLM APIs may reject unknown fields.
	for i := range out {
		out[i].Source = ""
	}
	return out
}

func (m *Manager) filterValidMessages(kept []types.Message) []types.Message {
	hasResult := map[string]bool{}
	for _, msg := range kept {
		// ClearedMarker still counts as "has result" — the tool was executed,
		// we just cleared old output. Excluding it would drop the assistant message.
		if msg.Role == "tool" && msg.ToolCallID != "" {
			hasResult[msg.ToolCallID] = true
		}
	}
	validAsst := map[int]bool{}
	for i, msg := range kept {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			ok := true
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" && !hasResult[tc.ID] {
					ok = false
					break
				}
			}
			if ok {
				validAsst[i] = true
			}
		}
	}
	validIDs := map[string]bool{}
	filtered := make([]types.Message, 0, len(kept))
	for i, msg := range kept {
		if msg.Content == "" && msg.Role != "system" {
			msg.Content = "."
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			if !validAsst[i] {
				continue
			}
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					validIDs[tc.ID] = true
				}
			}
		}
		if msg.Role == "tool" && (msg.ToolCallID == "" || !validIDs[msg.ToolCallID]) {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}
