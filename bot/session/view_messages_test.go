package session

import (
	"testing"

	"nekocode/bot/llm/types"
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

func TestDisplayMessagesKeepsDiffToolBlock(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "diff-call", Function: types.FunctionCall{Name: "diff", Arguments: `{"source":"/tmp/a.go"}`}},
			},
		},
		{Role: "tool", ToolCallID: "diff-call", Content: "[/tmp/a.go#diff]\n-1:old\n+1:new\n"},
	}
	got := DisplayMessages(msgs, 0)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("display messages = %+v, want diff block", got)
	}
	if got[0].Blocks[0].ToolName != "diff" || got[0].Blocks[0].Args != `{"source":"/tmp/a.go"}` {
		t.Fatalf("block = %+v, want diff args preserved", got[0].Blocks[0])
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

func TestDisplayMessagesCarriesToolArgs(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "bash-call", Function: types.FunctionCall{Name: "bash", Arguments: `{"command":"ls -la"}`}},
			},
		},
		{Role: "tool", ToolCallID: "bash-call", Content: "file.txt"},
	}
	got := DisplayMessages(msgs, 0)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("display messages = %+v, want one assistant bash block", got)
	}
	b := got[0].Blocks[0]
	if b.ToolName != "bash" || b.Args != `{"command":"ls -la"}` {
		t.Fatalf("block = %+v, want bash command args", b)
	}
}

func TestDisplayMessagesCarriesToolErrorState(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "bash-call", Function: types.FunctionCall{Name: "bash", Arguments: `{"command":"false"}`}},
			},
		},
		{Role: "tool", ToolCallID: "bash-call", Content: "command failed: exit status 1", IsError: true},
	}
	got := DisplayMessages(msgs, 0)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("display messages = %+v, want one assistant bash block", got)
	}
	if !got[0].Blocks[0].IsError {
		t.Fatalf("block = %+v, want IsError=true", got[0].Blocks[0])
	}
}
