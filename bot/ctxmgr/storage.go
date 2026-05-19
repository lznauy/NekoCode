package ctxmgr

import (
	"nekocode/bot/ctxmgr/compact"
	"nekocode/llm"
)

func summary(s string) string {
	const maxLen = 80
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (m *Manager) Add(role, content string) {
	compact.Log("add_msg: role=%s len=%d content=%q", role, len(content), summary(content))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Messages = append(m.ctx.Messages, llm.Message{Role: role, Content: content})
	m.tok.Tracker.AddNew(len(role) + len(content))
}

func (m *Manager) AddAssistantResponse(content, reasoning string) {
	compact.Log("add_assistant: len=%d reasoning=%d", len(content), len(reasoning))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Messages = append(m.ctx.Messages, llm.Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoning,
	})
	m.tok.Tracker.AddNew(len("assistant") + len(content) + len(reasoning))
}

func (m *Manager) AddAssistantToolCall(content, reasoning string, toolCalls []llm.ToolCall) {
	var names []string
	for _, tc := range toolCalls {
		names = append(names, tc.Function.Name)
	}
	compact.Log("add_assistant_tool: len=%d tools=%v", len(content), names)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Messages = append(m.ctx.Messages, llm.Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
	})
	m.tok.Tracker.AddNew(len("assistant") + len(content) + len(reasoning))
}

func (m *Manager) AddToolResult(toolCallID, content, toolName string) {
	compact.Log("add_tool_result: tool=%s len=%d", toolName, len(content))
	m.mu.Lock()
	defer m.mu.Unlock()
	role := "tool"
	if toolCallID == "" {
		role = "user"
	}
	content, _ = compact.BudgetResult(content, toolName)
	m.ctx.Messages = append(m.ctx.Messages, llm.Message{
		Role:       role,
		Content:    content,
		ToolCallID: toolCallID,
	})
	m.tok.Tracker.AddNew(len(role) + len(content) + len(toolCallID))
}

type ToolResultMsg struct {
	Message  llm.Message
	ToolName string
}

func (m *Manager) AddToolResultsBatch(results []ToolResultMsg) {
	var names []string
	for _, r := range results {
		names = append(names, r.ToolName)
	}
	compact.Log("add_tool_results_batch: tools=%v count=%d", names, len(results))
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range results {
		role := "tool"
		if r.Message.ToolCallID == "" {
			role = "user"
		}
		content, _ := compact.BudgetResult(r.Message.Content, r.ToolName)
		m.ctx.Messages = append(m.ctx.Messages, llm.Message{
			Role:       role,
			Content:    content,
			ToolCallID: r.Message.ToolCallID,
		})
		m.tok.Tracker.AddNew(len(role) + len(content) + len(r.Message.ToolCallID))
	}
}

func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Messages = make([]llm.Message, 0)
	m.ctx.CompactBoundary = 0
	m.ctx.Todo = ""
	m.ctx.TodoItems = nil
}
