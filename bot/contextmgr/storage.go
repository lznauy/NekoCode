package contextmgr

import (
	"nekocode/bot/contextmgr/compact"
	"nekocode/bot/debug"
	"nekocode/bot/llm/types"
)

func summary(s string) string {
	const maxLen = 80
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (m *Manager) Add(role, content string, source ...string) {
	s := ""
	if len(source) > 0 {
		s = source[0]
	}
	debug.Log("add_msg: role=%s source=%s len=%d content=%q", role, s, len(content), summary(content))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Messages = append(m.ctx.Messages, types.Message{Role: role, Content: content, Source: s})
	m.Tracker.AddNew(len(role) + len(content))
}

func (m *Manager) AddAssistantResponse(content, reasoning string) {
	debug.Log("add_assistant: len=%d reasoning=%d", len(content), len(reasoning))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Messages = append(m.ctx.Messages, types.Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoning,
	})
	m.Tracker.AddNew(len("assistant") + len(content) + len(reasoning))
}

func (m *Manager) AddAssistantToolCall(content, reasoning string, toolCalls []types.ToolCall) {
	var names []string
	for _, tc := range toolCalls {
		names = append(names, tc.Function.Name)
	}
	debug.Log("add_assistant_tool: len=%d tools=%v", len(content), names)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Messages = append(m.ctx.Messages, types.Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
	})
	tcBytes := 0
	for _, tc := range toolCalls {
		tcBytes += len(tc.ID) + len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	m.Tracker.AddNew(len("assistant") + len(content) + len(reasoning) + tcBytes)
}

type ToolResultMsg struct {
	Message  types.Message
	ToolName string
}

func (m *Manager) AddToolResultsBatch(results []ToolResultMsg) {
	var names []string
	for _, r := range results {
		names = append(names, r.ToolName)
	}
	debug.Log("add_tool_results_batch: tools=%v count=%d", names, len(results))
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range results {
		role := "tool"
		if r.Message.ToolCallID == "" {
			role = "user"
		}
		content, _ := compact.BudgetResult(r.Message.Content, r.ToolName)
		m.ctx.Messages = append(m.ctx.Messages, types.Message{
			Role:       role,
			Content:    content,
			ToolCallID: r.Message.ToolCallID,
			IsError:    r.Message.IsError,
		})
		m.Tracker.AddNew(len(role) + len(content) + len(r.Message.ToolCallID))
	}
}

func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearInternal()
}

// clearInternal clears messages, todo, and compact boundary. Caller must hold mu.
func (m *Manager) clearInternal() {
	m.ctx.Messages = make([]types.Message, 0)
	m.ctx.CompactBoundary = 0
	m.ctx.Todo = ""
	m.ctx.TodoItems = nil
}
