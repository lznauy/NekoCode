package agent

import (
	"errors"
	"nekocode/util/modelbudget"
	"testing"
)

func TestSharedBudgetExhaustionIsRunFailureNotAssistantAnswer(t *testing.T) {
	a, llm := newTestAgentWithLLM()
	budget, _ := modelbudget.New(1)
	ctx := modelbudget.WithRole(modelbudget.WithBudget(t.Context(), budget), modelbudget.Coordinator, "")
	if err := modelbudget.Admit(ctx); err != nil {
		t.Fatal(err)
	}
	result := a.loopRunner.runContext(ctx, "review evidence", nil, nil)
	if !errors.Is(result.Error, modelbudget.ErrExhausted) || result.StopReason != "call_budget" || llm.calls != 0 || result.FinalOutput != "" {
		t.Fatalf("budget error became normal answer: %+v calls=%d", result, llm.calls)
	}
}

func TestSharedBudgetExhaustionDuringFinalSummary(t *testing.T) {
	a, llm := newTestAgentWithLLM()
	budget, _ := modelbudget.New(1)
	ctx := modelbudget.WithRole(modelbudget.WithBudget(t.Context(), budget), modelbudget.Coordinator, "")
	if err := modelbudget.Admit(ctx); err != nil {
		t.Fatal(err)
	}
	a.life.SetRunContext(ctx)
	a.loopRunner.stepLimitHit = true
	result := a.loopRunner.finishRun(nil)
	a.loopRunner.publishSummary(result, nil)
	if !errors.Is(result.Error, modelbudget.ErrExhausted) || result.StopReason != "call_budget" || result.FinalOutput != "" || llm.calls != 0 {
		t.Fatalf("final summary lost admission failure: %+v, calls=%d", result, llm.calls)
	}
}
