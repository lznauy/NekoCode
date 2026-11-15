package contextmgr

import (
	"errors"
	"nekocode/bot/provider/types"
	"nekocode/util/modelbudget"
	"strings"
	"testing"
)

func TestHandoffAvoidsCompactionButNeverBypassesContextLimit(t *testing.T) {
	calls := 0
	m := New(Config{ContextWindow: 10000, AutoCompactPercent: 1, Summarizer: func([]types.Message, string) (string, error) { calls++; return "summary", nil }})
	for range 4 {
		m.Add("user", strings.Repeat("question ", 100))
		m.Add("assistant", strings.Repeat("evidence ", 100))
	}
	before := m.Snapshot()
	messages, err := m.PrepareRequest(t.Context(), ModelRequest{HandoffOnly: true, OutputTokens: 100})
	if err != nil || len(messages) == 0 || calls != 0 {
		t.Fatalf("handoff unexpectedly compacted: calls=%d err=%v", calls, err)
	}
	m.Add("user", strings.Repeat("oversized ", 10000))
	_, err = m.PrepareRequest(t.Context(), ModelRequest{HandoffOnly: true, OutputTokens: 100})
	var budget *RequestBudgetError
	if !errors.As(err, &budget) || calls != 0 || len(m.Snapshot().Messages) <= len(before.Messages) {
		t.Fatal("oversized handoff was admitted or history lost")
	}
}

func TestSharedBudgetNoticeIncludedInContextAdmission(t *testing.T) {
	m := New(Config{ContextWindow: 2000})
	budget, _ := modelbudget.New(100)
	messages, err := m.PrepareRequest(modelbudget.WithBudget(t.Context(), budget), ModelRequest{HandoffOnly: true, OutputTokens: 100})
	if err != nil || !strings.Contains(messages[len(messages)-1].Content, "Shared execution budget") {
		t.Fatalf("missing budget notice: %v", err)
	}
	if len(m.Snapshot().Messages) != 0 {
		t.Fatal("live budget counters polluted durable history")
	}
}

func TestExhaustedWorkBudgetPreservesCoordinatorSynthesis(t *testing.T) {
	budget, _ := modelbudget.New(100)
	work := modelbudget.WithBudget(t.Context(), budget)
	for modelbudget.Admit(work) == nil {
	}
	ctx := modelbudget.WithRole(work, modelbudget.Coordinator, "")
	client := &usageTestClient{response: &types.Response{Choices: []types.Choice{{Message: types.Message{Content: "saved evidence summary"}}}}}
	m := New(Config{ContextWindow: 10000, CompactionModel: client}) // Default automatic threshold: 80%.
	for range 18 {
		m.Add("user", strings.Repeat("question ", 100))
		m.Add("assistant", strings.Repeat("evidence ", 100))
	}
	if m.Status().Tokens < 8000 {
		t.Fatal("fixture did not reach the default compaction threshold")
	}
	before := budget.Snapshot()
	messages, err := m.PrepareRequest(ctx, ModelRequest{OutputTokens: 100})
	if err != nil || len(messages) == 0 {
		t.Fatalf("fitting final synthesis was blocked: %v", err)
	}
	if budget.Snapshot() != before {
		t.Fatal("request preparation consumed a reserve")
	}
	if !errors.Is(modelbudget.Admit(work), modelbudget.ErrExhausted) {
		t.Fatal("ordinary work consumed the coordinator reserve")
	}
	if err := modelbudget.Admit(ctx); err != nil {
		t.Fatalf("coordinator could not use its reserve: %v", err)
	}
	m.Add("user", strings.Repeat("oversized ", 10000))
	_, err = m.PrepareRequest(ctx, ModelRequest{OutputTokens: 100})
	var window *RequestBudgetError
	if !errors.As(err, &window) || window.Total() <= window.Window {
		t.Fatalf("oversized synthesis was admitted: %v", err)
	}
	if budget.Snapshot().CoordinatorRemaining != before.CoordinatorRemaining-1 {
		t.Fatal("failed compaction spent coordinator reserve")
	}
}
