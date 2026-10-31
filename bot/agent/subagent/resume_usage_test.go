package subagent

import (
	"context"
	"io"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
)

func TestResultUsesPerCallUsageWithEstimateFallback(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		scripts                    [][]types.StreamToken
		wantPrompt, wantCompletion int
	}{
		{"reported", [][]types.StreamToken{append(completionScript(), types.StreamToken{Usage: &types.StreamUsage{PromptTokens: 100, CompletionTokens: 10}})}, 100, 10},
		{"estimate", [][]types.StreamToken{completionScript()}, 0, 1},
		{"reported_less_than_estimate", [][]types.StreamToken{append(append([]types.StreamToken{{ReasoningContent: "inspect"}, {Content: "ready"}}, completionScript()...), types.StreamToken{Usage: &types.StreamUsage{PromptTokens: 10, CompletionTokens: 1}})}, 10, 1},
		{"multiple_calls", [][]types.StreamToken{
			{{Content: "inspecting"}, {Usage: &types.StreamUsage{PromptTokens: 20, CompletionTokens: 5}}},
			append(completionScript(), types.StreamToken{Usage: &types.StreamUsage{PromptTokens: 30, CompletionTokens: 6}}),
		}, 50, 11},
		{"mixed_calls", [][]types.StreamToken{
			{{Content: "inspecting"}},
			append(completionScript(), types.StreamToken{Usage: &types.StreamUsage{PromptTokens: 30, CompletionTokens: 6}}),
		}, 30, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			llm := &scriptedLLM{scripts: tc.scripts}
			var prompt, completion int
			var saved AttemptUsage
			result, err := New(Config{LLM: llm, Tools: tools.New()}).Run(t.Context(), RunConfig{
				Prompt: "inspect and finish", Profile: Profile{Name: "explore"},
				AddTokens:   func(p, c int) { prompt += p; completion += c },
				SaveContext: func(_ ctxmgr.ManagerSnapshot, _, _ string, usage AttemptUsage) error { saved = usage; return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			want := tc.wantPrompt + tc.wantCompletion
			if result.TotalTokens != want || saved.TotalTokens != want || prompt != tc.wantPrompt || completion != tc.wantCompletion {
				t.Fatalf("result=%d saved=%d callback=%d/%d; want %d/%d", result.TotalTokens, saved.TotalTokens, prompt, completion, tc.wantPrompt, tc.wantCompletion)
			}
		})
	}
}

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
	if result.TotalTokens != 35 {
		t.Fatalf("current run tokens = %d, want 35", result.TotalTokens)
	}
	if persisted.Tracker.CacheHitTokens != 120 || persisted.Tracker.CacheMissTokens != 60 {
		t.Fatalf("persisted tracker lost cumulative usage: %+v", persisted.Tracker)
	}
}

func TestResultIncludesUsageFromRetriedCalls(t *testing.T) {
	llm := &scriptedLLM{
		scripts: [][]types.StreamToken{
			{{Content: "partial"}, {Usage: &types.StreamUsage{PromptTokens: 20, CompletionTokens: 5}}},
			append(completionScript(), types.StreamToken{Usage: &types.StreamUsage{PromptTokens: 30, CompletionTokens: 6}}),
		},
		errors: []error{io.ErrUnexpectedEOF},
	}
	result, err := New(Config{LLM: llm, Tools: tools.New()}).Run(t.Context(), RunConfig{
		Prompt: "inspect and finish", Profile: Profile{Name: "explore"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 2 || result.TotalTokens != 61 {
		t.Fatalf("calls=%d tokens=%d; want 2 calls and 61 tokens", llm.calls, result.TotalTokens)
	}
}
