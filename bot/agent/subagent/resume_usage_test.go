package subagent

import (
	"context"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
)

func TestResumeResultReportsOnlyCurrentRunCache(t *testing.T) {
	snapshot := ctxmgr.ManagerSnapshot{Tracker: token.State{CacheHitTokens: 100, CacheMissTokens: 50}}
	script := completionScript()
	script = append(script, types.StreamToken{Usage: &types.StreamUsage{PromptTokens: 30, CompletionTokens: 5, CacheHitTokens: 20, CacheMissTokens: 10, CacheUsageReported: true}})
	llm := &scriptedLLM{scripts: [][]types.StreamToken{script}}
	var persisted ctxmgr.ManagerSnapshot
	result, err := New(Config{LLM: llm, Tools: tools.New()}).Run(context.Background(), RunConfig{Prompt: "verify fix", Profile: Profile{Name: "explore"}, ResumeContext: &snapshot, SaveContext: func(s ctxmgr.ManagerSnapshot, _, _ string, _ AttemptUsage) error { persisted = s; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheHitTokens != 20 || result.CacheMissTokens != 10 {
		t.Fatalf("current run cache hit/miss = %d/%d, want 20/10; prior run usage must not be charged to parent again", result.CacheHitTokens, result.CacheMissTokens)
	}
	if persisted.Tracker.CacheHitTokens != 120 || persisted.Tracker.CacheMissTokens != 60 {
		t.Fatalf("persisted tracker lost cumulative usage: %+v", persisted.Tracker)
	}
}
