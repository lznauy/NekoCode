package llmstream

import (
	"errors"
	"nekocode/bot/calllog"
	"nekocode/bot/provider/types"
	"nekocode/util/modelbudget"
	"testing"
)

func TestAdmissionCountsThinkingRetryButDoesNotLogDeniedRequest(t *testing.T) {
	budget, _ := modelbudget.New(2) // one ordinary request; one coordinator reserve
	ctx := modelbudget.WithBudget(t.Context(), budget)
	client := &glmFakeClient{responses: [][]types.StreamToken{{{ReasoningContent: "reasoning only"}}}}
	records := 0
	old := writeRecord
	writeRecord = func(calllog.Record) { records++ }
	defer func() { writeRecord = old }()
	result, err := CallLLMWithRetry(ctx, client, func() LLMCallOptions { return LLMCallOptions{Ctx: ctx} })
	if !errors.Is(err, modelbudget.ErrExhausted) || result == nil || client.calls != 1 || records != 1 || budget.Snapshot().Admitted != 1 {
		t.Fatalf("retry overran budget: calls=%d records=%d err=%v", client.calls, records, err)
	}
	_, err = CallLLM(client, LLMCallOptions{Ctx: ctx})
	if !errors.Is(err, modelbudget.ErrExhausted) || records != 1 {
		t.Fatalf("denial generated a physical record: %v %d", err, records)
	}
}

func TestReportedTokensAreAdvisoryAcrossCalls(t *testing.T) {
	budget, err := modelbudget.NewWithLimits(20, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx := modelbudget.WithBudget(t.Context(), budget)
	usage := types.StreamUsage{PromptTokens: 12, CompletionTokens: 3}
	client := &glmFakeClient{responses: [][]types.StreamToken{{{Content: "first", Usage: &usage}}, {{Content: "second"}}}}
	for range 2 {
		if _, err := CallLLM(client, LLMCallOptions{Ctx: ctx}); err != nil {
			t.Fatal(err)
		}
	}
	if s := budget.Snapshot(); s.ObservedTokens != 15 || s.Admitted != 2 {
		t.Fatalf("unexpected budget: %+v", s)
	}
}
