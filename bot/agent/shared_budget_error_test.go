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
	if !errors.Is(result.Error, modelbudget.ErrExhausted) || llm.calls != 0 || result.FinalOutput != "" {
		t.Fatalf("budget error became normal answer: %+v calls=%d", result, llm.calls)
	}
}
