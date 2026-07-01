package contextmgr

import (
	"testing"

	"nekocode/bot/llm/types"
)

func TestFilterValidMessages_PassesNormal(t *testing.T) {
	m := newManager()
	m.Add("user", "hello")
	m.AddAssistantResponse("reply", "")
	msgs := m.Build()
	if len(msgs) < 2 {
		t.Errorf("expected at least 2 messages in build output, got %d", len(msgs))
	}
}

func TestFilterValidMessages_OrphanToolsDropped(t *testing.T) {
	m := newManager()
	// Add a tool result with no matching assistant tool_call.
	m.AddToolResultsBatch([]ToolResultMsg{
		{Message: types.Message{Content: "orphan content", ToolCallID: "orphan-id"}, ToolName: "read"},
	})
	msgs := m.Build()
	for _, msg := range msgs {
		if msg.Role == "tool" && msg.ToolCallID == "orphan-id" {
			t.Error("orphan tool result should have been filtered out")
		}
	}
}

func TestFilterValidMessages_AssistantWithToolCalls(t *testing.T) {
	m := newManager()
	m.AddAssistantToolCall("I'll read", "", []types.ToolCall{
		{ID: "tc1", Type: "function", Function: types.FunctionCall{Name: "read", Arguments: "{}"}},
	})
	m.AddToolResultsBatch([]ToolResultMsg{
		{Message: types.Message{Content: "file content", ToolCallID: "tc1"}, ToolName: "read"},
	})

	msgs := m.Build()
	foundAsst, foundTool := false, false
	for _, msg := range msgs {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			foundAsst = true
		}
		if msg.Role == "tool" && msg.ToolCallID == "tc1" {
			foundTool = true
		}
	}
	if !foundAsst || !foundTool {
		t.Error("valid assistant→tool chain should be preserved")
	}
}

func TestFilterValidMessages_EmptyContent(t *testing.T) {
	m := newManager()
	m.ctx.Messages = append(m.ctx.Messages, types.Message{Role: "assistant", Content: ""})
	msgs := m.Build()
	for _, msg := range msgs {
		if msg.Role == "assistant" && msg.Content == "" {
			t.Error("empty assistant messages should get '.' placeholder")
		}
	}
}

func TestBuild_EmptyManager(t *testing.T) {
	m := newManager()
	msgs := m.Build()
	if len(msgs) == 0 {
		t.Error("Build should return at least system prompt")
	}
}
