package standard

import (
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/core"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
)

func TestSubagentHistoryKeepsReadToolAndMarksContextHeader(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := session.OpenSubagent("parent", "child", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := child.Begin("explore", "inspect", nil, "检查认证"); err != nil {
		t.Fatal(err)
	}
	messages := []types.Message{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "read-1", Function: types.FunctionCall{Name: "read", Arguments: `{"path":"a.go"}`}}}},
		{Role: "tool", ToolCallID: "read-1", Content: "permission denied", IsError: true},
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{Messages: messages, Transcript: messages}, "completed", "done", session.SubagentAttemptUsage{CacheHitTokens: 80, CacheMissTokens: 20, ContextTokens: 1500, ContextWindow: 200000, ToolUseCount: 7}); err != nil {
		t.Fatal(err)
	}
	adapter := &adapter{bot: &core.Bot{}}
	page, err := adapter.SubagentHistory("parent", "child", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || !page.Records[0].ContextOnly || page.Records[0].ToolCalls[0].Name != "read" {
		t.Fatalf("lost tool header: %+v", page)
	}
	result := page.Records[1]
	if result.Sequence != 3 || result.ContextOnly || !result.IsError || result.Content != "permission denied" || result.ToolCallID != "read-1" {
		t.Fatalf("lost chronological tool result: %+v", result)
	}
	summaries, err := adapter.Subagents("parent")
	if err != nil || len(summaries) != 1 || summaries[0].LatestAttempt == nil || summaries[0].LatestAttempt.Seq != 1 || summaries[0].LatestAttempt.Result != "done" || summaries[0].Title != "检查认证" || summaries[0].LatestAttempt.ContextTokens != 1500 || summaries[0].LatestAttempt.ContextWindow != 200000 || summaries[0].LatestAttempt.ToolUseCount != 7 {
		t.Fatalf("latest attempt not projected: %+v err=%v", summaries, err)
	}
	attempts, err := adapter.SubagentAttempts("parent", "child", 0, 10)
	if err != nil || len(attempts.Attempts) != 1 || attempts.Attempts[0].ID != summaries[0].LatestAttempt.ID {
		t.Fatalf("attempt page mismatch: %+v err=%v", attempts, err)
	}
}
