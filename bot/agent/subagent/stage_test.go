package subagent

import (
	"encoding/json"
	ctxmgr "nekocode/bot/contextmgr"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
	"nekocode/util/modelbudget"
	"strings"
	"testing"
)

func stageHandoff() []types.StreamToken {
	args := handoffArgs()
	args["unfinished"] = []any{"continue call-site validation; stop after one reachable reproduction or report blocked"}
	raw, _ := json.Marshal(args)
	return []types.StreamToken{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "handoff", Name: submitResultToolName, Arguments: string(raw)}}}
}
func TestStagePersistsPartialThenResumesOriginalContext(t *testing.T) {
	var snapshot ctxmgr.ManagerSnapshot
	var saved string
	worker := New(Config{LLM: &scriptedLLM{scripts: [][]types.StreamToken{{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "inspect", Name: "inspect", Arguments: `{}`}}}, stageHandoff()}}, Tools: tools.New(&countingTool{name: "inspect"})})
	cfg := RunConfig{SubagentID: "same-task", AttemptID: "attempt-1", Prompt: "review full scope", Profile: Profile{Name: "executor", Tools: []string{"inspect"}, MaxSteps: 1}, SaveContext: func(s ctxmgr.ManagerSnapshot, status, _ string, _ AttemptUsage) error {
		snapshot = s
		saved = status
		return nil
	}}
	result, err := worker.Run(t.Context(), cfg)
	if err != nil || result.Status != StatusPartial || result.Summary.StopReason != "step_limit" || saved != "partial" || result.Handoff == nil {
		t.Fatalf("%+v %v saved=%s", result, err, saved)
	}
	cfg.ResumeContext = &snapshot
	cfg.AttemptID = "attempt-2"
	cfg.Prompt = "validate only remaining call site"
	args := handoffArgs()
	raw, _ := json.Marshal(args)
	resumed := New(Config{LLM: &scriptedLLM{scripts: [][]types.StreamToken{{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "final", Name: submitResultToolName, Arguments: string(raw)}}}}}, Tools: tools.New(&countingTool{name: "inspect"})})
	result, err = resumed.Run(t.Context(), cfg)
	if err != nil || result.Status != StatusCompleted || saved != "completed" {
		t.Fatalf("resume: %+v %v", result, err)
	}
	data, _ := json.Marshal(snapshot)
	if !strings.Contains(string(data), "inspect") || !strings.Contains(string(data), "call-site") {
		t.Fatal("original tool/evidence context was lost")
	}
}
func TestSharedExhaustionAllowsWorkerHandoff(t *testing.T) {
	b, _ := modelbudget.New(20)
	ctx := modelbudget.WithBudget(t.Context(), b)
	for b.Snapshot().WorkRemaining > 0 {
		if err := modelbudget.Admit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	worker := New(Config{LLM: &scriptedLLM{scripts: [][]types.StreamToken{stageHandoff()}}, Tools: tools.New()})
	result, err := worker.Run(ctx, RunConfig{SubagentID: "task", AttemptID: "attempt", Profile: Profile{Name: "executor"}, Prompt: "review"})
	if err != nil || result.Status != StatusPartial || result.Summary.StopReason != "shared_budget" || result.Handoff == nil {
		t.Fatalf("%+v %v", result, err)
	}
	if b.Snapshot().Admitted != 16 {
		t.Fatal(b.Snapshot())
	}
}
