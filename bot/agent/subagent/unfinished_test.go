package subagent

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
	"testing"
)

func TestUnfinishedHandoffIsPartialIncludingBudgetFinalization(t *testing.T) {
	for _, limit := range []int{0, 1} {
		t.Run(map[int]string{0: "ordinary submission", 1: "budget finalization"}[limit], func(t *testing.T) {
			completion := []types.StreamToken{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "handoff", Name: submitResultToolName, Arguments: `{"summary":"investigation handed off","evidence":[],"files":[],"verification":"remaining tests blocked","unfinished":["run remaining regression"],"risks":[]}`}}}
			scripts := [][]types.StreamToken{completion}
			if limit != 0 {
				scripts = append([][]types.StreamToken{{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "inspect", Name: "inspect", Arguments: `{}`}}}}, scripts...)
			}
			var saved string
			result, err := New(Config{LLM: &scriptedLLM{scripts: scripts}, Tools: tools.New(&countingTool{name: "inspect"})}).Run(t.Context(), RunConfig{
				Prompt: "investigate and verify", Profile: Profile{Name: "test", Tools: []string{"inspect"}, MaxSteps: limit},
				SaveContext: func(_ ctxmgr.ManagerSnapshot, status, _ string, _ AttemptUsage) error { saved = status; return nil },
			})
			if err != nil || result.Status != StatusPartial || saved != "partial" {
				t.Fatalf("unfinished work marked complete: result=%+v saved=%q err=%v", result, saved, err)
			}
			if result.Handoff == nil || len(result.Handoff.Unfinished) != 1 {
				t.Fatal("unfinished evidence was lost")
			}
		})
	}
}
