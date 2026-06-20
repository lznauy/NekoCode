package sessionview

import (
	"testing"

	"nekocode/llm/types"
)

func TestDisplayMessagesKeepsPersistentToolBlocks(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "read-call", Function: types.FunctionCall{Name: "read"}},
				{ID: "edit-call", Function: types.FunctionCall{Name: "edit"}},
			},
		},
		{Role: "tool", ToolCallID: "read-call", Content: "read output"},
		{Role: "tool", ToolCallID: "edit-call", Content: "edit output"},
	}
	got := DisplayMessages(msgs, 0)
	if len(got) != 1 || got[0].Content != "" {
		t.Fatalf("display messages = %+v, want one assistant tool block message", got)
	}
	if len(got[0].Blocks) != 1 || got[0].Blocks[0].ToolName != "edit" || got[0].Blocks[0].Content != "edit output" {
		t.Fatalf("display messages = %+v, want edit block", got)
	}
}

func TestDisplayMessagesFiltersInternalMessages(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Source: "hint", Content: "hidden"},
		{Role: "user", Content: "visible"},
	}
	got := DisplayMessages(msgs, 0)
	if len(got) != 1 || got[0].Content != "visible" {
		t.Fatalf("display messages = %+v, want visible user only", got)
	}
}
