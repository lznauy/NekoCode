package sessionview

import (
	"strings"

	"nekocode/common"
	"nekocode/llm/types"
)

func DisplayMessages(messages []types.Message, compactBoundary int) []common.DisplayMessage {
	if compactBoundary > 0 && compactBoundary < len(messages) {
		messages = messages[compactBoundary:]
	}

	toolNames := toolNamesByID(messages)
	var out []common.DisplayMessage
	i := 0
	for i < len(messages) {
		m := messages[i]
		switch m.Role {
		case "user":
			if !isInternalMessage(m) {
				out = append(out, common.DisplayMessage{Role: "user", Content: m.Content})
			}
			i++
		case "assistant":
			msg, next := displayAssistantMessage(messages, i, toolNames)
			if msg.Content != "" || len(msg.Blocks) > 0 {
				out = append(out, msg)
			}
			i = next
		case "system":
			if !isInternalMessage(m) {
				out = append(out, common.DisplayMessage{Role: "system", Content: m.Content})
			}
			i++
		default:
			i++
		}
	}
	return out
}

func toolNamesByID(msgs []types.Message) map[string]string {
	toolNames := make(map[string]string, len(msgs))
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				toolNames[tc.ID] = tc.Function.Name
			}
		}
	}
	return toolNames
}

func displayAssistantMessage(msgs []types.Message, idx int, toolNames map[string]string) (common.DisplayMessage, int) {
	m := msgs[idx]
	var blocks []common.DisplayBlock
	next := idx + 1
	if len(m.ToolCalls) > 0 {
		for next < len(msgs) && msgs[next].Role == "tool" {
			name := toolNames[msgs[next].ToolCallID]
			if isPersistentTool(name) {
				blocks = append(blocks, common.DisplayBlock{
					ToolName: name,
					Content:  msgs[next].Content,
				})
			}
			next++
		}
	}

	content := m.Content
	if len(m.ToolCalls) > 0 {
		content = ""
	}
	return common.DisplayMessage{
		Role:    "assistant",
		Content: content,
		Blocks:  blocks,
	}, next
}

func isPersistentTool(name string) bool {
	return name == "edit" || name == "write" || name == "bash"
}

func isInternalMessage(msg types.Message) bool {
	return msg.Source == "hint" ||
		strings.Contains(msg.Content, "<hints>") ||
		strings.Contains(msg.Content, "<skill") ||
		strings.Contains(msg.Content, "Current working directory") ||
		strings.Contains(msg.Content, "<system-reminder>") ||
		strings.HasPrefix(msg.Content, "[Hook:")
}
