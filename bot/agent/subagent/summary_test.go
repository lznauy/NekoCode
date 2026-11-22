package subagent

import (
	"context"
	"errors"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestRunSummaryMatchesExitAndFinalCheckpoint(t *testing.T) {
	toolCall := []types.StreamToken{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "inspect", Name: "inspect", Arguments: `{}`}}}
	for _, tc := range []struct {
		name                string
		scripts             [][]types.StreamToken
		errors              []error
		limit               int
		cancel, saveFailure bool
		want                protocol.RunSummary
	}{
		{name: "completed", scripts: [][]types.StreamToken{completionScript()}, want: protocol.RunSummary{StepCount: 1, StopReason: "completed"}},
		{name: "incomplete", scripts: [][]types.StreamToken{{{Content: "progress"}}, {{Content: "progress"}}, {{Content: "progress"}}}, want: protocol.RunSummary{StepCount: 3, StopReason: "incomplete"}},
		{name: "step limit", limit: 1, scripts: [][]types.StreamToken{toolCall, completionScript()}, want: protocol.RunSummary{StepCount: 2, StopReason: "step_limit"}},
		{name: "cancelled", cancel: true, want: protocol.RunSummary{StopReason: "cancelled"}},
		{name: "provider cancelled", scripts: [][]types.StreamToken{{}}, errors: []error{context.Canceled}, want: protocol.RunSummary{StepCount: 1, StopReason: "execution_error"}},
		{name: "provider timeout", scripts: [][]types.StreamToken{{}}, errors: []error{context.DeadlineExceeded}, want: protocol.RunSummary{StepCount: 1, StopReason: "execution_error"}},
		{name: "save failure", saveFailure: true, scripts: [][]types.StreamToken{completionScript()}, want: protocol.RunSummary{StepCount: 1, StopReason: "execution_error"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			model := &scriptedLLM{scripts: tc.scripts, errors: tc.errors}
			var saved protocol.RunSummary
			result, err := New(Config{LLM: model, Tools: tools.New(testProxyTool{name: "inspect"})}).Run(ctx, RunConfig{
				Prompt: "inspect", Profile: Profile{Name: "test", Tools: []string{"inspect"}, MaxSteps: tc.limit},
				SaveContext: func(_ ctxmgr.ManagerSnapshot, status, _ string, usage AttemptUsage) error {
					if status != "running" {
						if usage.Summary.StopReason == "cancelled" && status != "interrupted" {
							t.Fatalf("cancelled summary saved as %q", status)
						}
						saved = usage.Summary
						if tc.saveFailure {
							return errors.New("save failed")
						}
					}
					return nil
				},
			})
			if result == nil || result.Summary != tc.want {
				t.Fatalf("result=%+v error=%v want=%+v", result, err, tc.want)
			}
			if !tc.saveFailure && saved != tc.want {
				t.Fatalf("saved=%+v want=%+v", saved, tc.want)
			}
		})
	}
}

func TestFinalCheckpointCommitsOutcomeOnlyOnSuccess(t *testing.T) {
	for _, mode := range []string{"cancelled error", "deadline error", "late context cancellation"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var cause error
			var saved protocol.RunSummary
			if mode == "cancelled error" {
				cause = context.Canceled
			}
			if mode == "deadline error" {
				cause = context.DeadlineExceeded
			}
			result, err := New(Config{LLM: &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}, Tools: tools.New()}).Run(ctx, RunConfig{
				Prompt: "inspect", Profile: Profile{Name: "test"},
				SaveContext: func(_ ctxmgr.ManagerSnapshot, status, _ string, usage AttemptUsage) error {
					if status == "running" {
						return nil
					}
					if cause == nil {
						saved = usage.Summary
						cancel()
					}
					return cause
				},
			})
			want := "execution_error"
			if mode == "late context cancellation" {
				want = "completed"
				if err != nil || result == nil || result.Status != StatusCompleted || saved != result.Summary {
					t.Fatalf("lost committed completion: %+v, %v", result, err)
				}
			}
			if result == nil || result.Summary.StopReason != want || result.Summary.StepCount != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if cause != nil && (!errors.Is(err, cause) || !protocol.EvidenceIncomplete(err)) {
				t.Fatalf("save failure cause lost: %v", err)
			}
		})
	}
}
