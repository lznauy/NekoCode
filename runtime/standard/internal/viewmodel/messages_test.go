package viewmodel

import (
	"testing"

	"nekocode/bot/provider/types"
	controlruntime "nekocode/runtime"
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
	got := DisplayMessages(msgs)
	if len(got) != 1 || got[0].Content != "" {
		t.Fatalf("display messages = %+v, want one assistant tool block message", got)
	}
	if len(got[0].Blocks) != 1 || got[0].Blocks[0].ToolName != "edit" || got[0].Blocks[0].Content != "edit output" {
		t.Fatalf("display messages = %+v, want edit block", got)
	}
}

func TestDisplayMessagesTreatsPartialTaskWithErrorAsPartial(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "task-call", Function: types.FunctionCall{Name: "task"}}}},
		{Role: "tool", ToolCallID: "task-call", Content: "[Subagent returned a partial result]\ncompleted evidence\nremaining error", IsError: true},
	}
	got := DisplayMessages(msgs)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("partial task produced failure blocks: %+v", got)
	}
	block := got[0].Blocks[0]
	if block.ToolName != "子任务" || block.Content != "未完成 1" || block.IsError {
		t.Fatalf("partial task summary = %+v", block)
	}
}

func TestDisplayMessagesHidesImageAttachmentMetadata(t *testing.T) {
	content := controlruntime.InputWithImageAttachments("分析图片 [Image #1]", []controlruntime.ImageAttachment{{
		Label: "[Image #1]", Path: "/tmp/paste.png",
	}}, false)
	msgs := []types.Message{{
		Role:    "user",
		Content: content,
	}}
	got := DisplayMessages(msgs)
	if len(got) != 1 || got[0].Content != "分析图片 [Image #1]" {
		t.Fatalf("display messages = %#v", got)
	}
}

func TestDisplayMessagesNormalizesLegacyTerminalOutput(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "shell-call", Function: types.FunctionCall{Name: "shell"}},
			},
		},
		{Role: "tool", ToolCallID: "shell-call", Content: "vite\n\rtransforming..."},
	}
	got := DisplayMessages(msgs)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("display messages = %+v, want one shell block", got)
	}
	if content := got[0].Blocks[0].Content; content != "vite\ntransforming..." {
		t.Fatalf("content = %q, want normalized terminal output", content)
	}
}

func TestDisplayMessagesCoalescesAssistantToolTurn(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "commit README"},
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "add-call", Function: types.FunctionCall{Name: "shell", Arguments: `{"command":"git add README.md"}`}},
			},
		},
		{Role: "tool", ToolCallID: "add-call", Content: ""},
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "commit-call", Function: types.FunctionCall{Name: "shell", Arguments: `{"command":"git commit -m docs"}`}},
			},
		},
		{Role: "tool", ToolCallID: "commit-call", Content: "Author identity unknown", IsError: true},
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "commit-retry-call", Function: types.FunctionCall{Name: "shell", Arguments: `{"command":"git -c user.name=NekoCode commit -m docs"}`}},
			},
		},
		{Role: "tool", ToolCallID: "commit-retry-call", Content: "[master e0e5307] docs"},
		{Role: "assistant", Content: "已提交。e0e5307。"},
	}

	got := DisplayMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("display messages len = %d, want user + one assistant: %+v", len(got), got)
	}
	assistant := got[1]
	if assistant.Role != "assistant" || assistant.Content != "已提交。e0e5307。" {
		t.Fatalf("assistant = %+v, want final content preserved", assistant)
	}
	if len(assistant.Blocks) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(assistant.Blocks), assistant.Blocks)
	}
	if assistant.Blocks[1].ToolName != "shell" || !assistant.Blocks[1].IsError {
		t.Fatalf("second block = %+v, want failed shell block", assistant.Blocks[1])
	}
	if assistant.Blocks[2].Content != "[master e0e5307] docs" {
		t.Fatalf("third block content = %q", assistant.Blocks[2].Content)
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
	got := DisplayMessages(msgs)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("display messages = %+v, want diff block", got)
	}
	if got[0].Blocks[0].ToolName != "diff" || got[0].Blocks[0].Args != `{"source":"/tmp/a.go"}` {
		t.Fatalf("block = %+v, want diff args preserved", got[0].Blocks[0])
	}
}

func TestDisplayMessagesKeepsProcessToolBlock(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "process-call", Function: types.FunctionCall{Name: "process", Arguments: `{"action":"wait","task":"server"}`}},
			},
		},
		{Role: "tool", ToolCallID: "process-call", Content: "status: done\nexit_code: 0"},
	}
	got := DisplayMessages(msgs)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("display messages = %+v, want process block", got)
	}
	if got[0].Blocks[0].ToolName != "process" || got[0].Blocks[0].Args != `{"action":"wait","task":"server"}` {
		t.Fatalf("block = %+v, want process args preserved", got[0].Blocks[0])
	}
}

func TestDisplayMessagesFiltersInternalMessages(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Source: "hint", Content: "hidden"},
		{Role: "user", Source: types.MessageSourceRuntimeContext, Content: "<runtime_context>hidden</runtime_context>"},
		{Role: "user", Source: types.MessageSourceRuntimeEvent, Content: "<workspace_event>hidden</workspace_event>"},
		{Role: "user", Content: "visible"},
	}
	got := DisplayMessages(msgs)
	if len(got) != 1 || got[0].Content != "visible" {
		t.Fatalf("display messages = %+v, want visible user only", got)
	}
}

func TestDisplayMessagesCarriesToolArgs(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "bash-call", Function: types.FunctionCall{Name: "shell", Arguments: `{"command":"ls -la"}`}},
			},
		},
		{Role: "tool", ToolCallID: "bash-call", Content: "file.txt"},
	}
	got := DisplayMessages(msgs)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("display messages = %+v, want one assistant bash block", got)
	}
	b := got[0].Blocks[0]
	if b.ToolName != "shell" || b.Args != `{"command":"ls -la"}` {
		t.Fatalf("block = %+v, want shell command args", b)
	}
}

func TestDisplayMessagesCarriesToolErrorState(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "bash-call", Function: types.FunctionCall{Name: "shell", Arguments: `{"command":"false"}`}},
			},
		},
		{Role: "tool", ToolCallID: "bash-call", Content: "command failed: exit status 1", IsError: true},
	}
	got := DisplayMessages(msgs)
	if len(got) != 1 || len(got[0].Blocks) != 1 {
		t.Fatalf("display messages = %+v, want one assistant bash block", got)
	}
	if !got[0].Blocks[0].IsError {
		t.Fatalf("block = %+v, want IsError=true", got[0].Blocks[0])
	}
}

func TestDisplayMessagesHidesPersistedAssistantReasoning(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", ReasoningContent: "inspect repository", ToolCalls: []types.ToolCall{{
			ID: "shell-call", Function: types.FunctionCall{Name: "shell"},
		}}},
		{Role: "tool", ToolCallID: "shell-call", Content: "ok"},
		{Role: "assistant", Content: "done", ReasoningContent: "summarize result"},
	}

	got := DisplayMessages(msgs)
	if len(got) != 1 || got[0].Content != "done" || got[0].Reasoning != "" {
		t.Fatalf("display messages = %+v, want content without persisted reasoning", got)
	}
}

func TestDisplayMessagesDropsReasoningOnlyAssistantTurn(t *testing.T) {
	got := DisplayMessages([]types.Message{{Role: "assistant", ReasoningContent: "partial thought"}})
	if len(got) != 0 {
		t.Fatalf("display messages = %+v, want reasoning-only turn hidden", got)
	}
}

func TestTaskHistoryPreservesSummaryFailuresAndResumeIdentity(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "do work"},
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "first", Function: types.FunctionCall{Name: "task"}},
			{ID: "failed", Function: types.FunctionCall{Name: "task"}},
		}},
		{Role: "tool", ToolCallID: "first", Content: "[Subagent returned a partial result]\n[Subagent ID: child]\npartial content"},
		{Role: "tool", ToolCallID: "failed", IsError: true, Content: "could not start"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "resume", Function: types.FunctionCall{Name: "task", Arguments: `{"resume_id":"child"}`}}}},
		{Role: "tool", ToolCallID: "resume", Content: "[Subagent ID: child]\ncompleted content"},
	}
	got := DisplayMessages(msgs)
	if len(got) != 2 || len(got[1].Blocks) != 2 {
		t.Fatalf("history = %+v", got)
	}
	blocks := got[1].Blocks
	if blocks[0].ToolName != "子任务失败" || !blocks[0].IsError || blocks[0].Content != "could not start" {
		t.Fatalf("failure = %+v", blocks[0])
	}
	if blocks[1].ToolName != "子任务" || blocks[1].Content != "完成 1 · 失败 1" {
		t.Fatalf("summary = %+v", blocks[1])
	}
	for offset := 0; offset < 2; offset++ {
		page, err := DisplayMessagesPage(msgs, offset, 1)
		if err != nil || page.Total != 2 || len(page.Messages) != 1 {
			t.Fatalf("page %d = %+v, %v", offset, page, err)
		}
	}
}
