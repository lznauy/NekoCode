package headless

import (
	"context"
	"errors"
	"fmt"
	"nekocode/protocol"
	rt "nekocode/runtime"
	"nekocode/util/modelbudget"
	"strconv"
	"testing"
)

func TestInitializeBudgetValidationAndPropagation(t *testing.T) {
	seen := make(chan int, 1)
	h := newHarness(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) {
		b := modelbudget.FromContext(ctx)
		if b == nil {
			seen <- 0
		} else {
			seen <- b.Snapshot().Limit
		}
		return "done", nil
	}, Options{})
	for _, value := range []any{nil, 0, -1, 1.5, "10", 1000001} {
		if f := rpc(t, h, "initialize", map[string]any{"model_call_budget": value}); f["subtype"] != "error" {
			t.Fatal(f)
		}
	}
	for _, invalid := range []map[string]any{{"external_token_limit": 100}, {"external_token_limit": nil, "model_call_budget": 100}, {"external_token_limit": -2, "model_call_budget": 100}, {"parallel_step_sharing": true}, {"parallel_step_sharing": "true", "model_call_budget": 100}} {
		if f := rpc(t, h, "initialize", invalid); f["subtype"] != "error" {
			t.Fatal(f)
		}
	}
	if f := rpc(t, h, "initialize", map[string]any{"model_call_budget": 100, "parallel_step_sharing": true, "external_token_limit": 999}); f["subtype"] != "success" {
		t.Fatal(f)
	}
	info := rpc(t, h, "server.info", nil)["response"].(map[string]any)
	if b := info["execution_budget"].(map[string]any); b["limit"] != float64(100) || b["parallel_step_sharing"] != true || b["external_token_limit"] != float64(999) {
		t.Fatal(info)
	}
	h.send(t, user("task", "review"))
	h.until(t, "result")
	if got := <-seen; got != 100 {
		t.Fatal(got)
	}
	h.finish(t)
}

func TestForegroundCoordinatorReclaimsHandoffsAndReportsFinalBudget(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) {
		worker := modelbudget.WithRole(ctx, modelbudget.Work, "child")
		for modelbudget.Admit(worker) == nil {
		}
		if modelbudget.FromContext(ctx).Snapshot().HandoffRemaining != 8 {
			return "", fmt.Errorf("worker reclaimed handoffs")
		}
		for i := 0; i < 6; i++ {
			handoff := modelbudget.WithRole(ctx, modelbudget.Handoff, strconv.Itoa(i))
			if err := modelbudget.Admit(handoff); err != nil {
				return "", err
			}
		}
		for i := 0; i < 6; i++ {
			if err := modelbudget.Admit(ctx); err != nil {
				return "", fmt.Errorf("denied coordinator call %d: %w", i, err)
			}
		}
		return "", fmt.Errorf("final request: %w", modelbudget.Admit(ctx))
	}, Options{})
	if f := rpc(t, h, "initialize", map[string]any{"model_call_budget": 400}); f["subtype"] != "success" {
		t.Fatal(f)
	}
	h.send(t, user("task", "review"))
	f := h.until(t, "result")
	if f["subtype"] != "error_call_budget" || f["stop_reason"] != "call_budget" {
		t.Fatal(f)
	}
	b := f["execution_budget"].(map[string]any)
	if b["admitted"] != float64(400) || b["work_remaining"] != float64(0) || b["handoff_remaining"] != float64(0) || b["coordinator_remaining"] != float64(0) {
		t.Fatal(b)
	}
	h.finish(t)
}

func TestTerminalFramePreservesMixedFailures(t *testing.T) {
	for _, tc := range []struct {
		err     error
		subtype string
	}{
		{errors.Join(modelbudget.ErrExhausted, protocol.MarkEvidenceIncomplete(errors.New("save failed"))), "error_during_execution"},
		{errors.Join(modelbudget.ErrExhausted, errors.New("save failed")), "error_during_execution"},
		{fmt.Errorf("provider: %w", protocol.ErrIncompleteResponse), "error_incomplete"},
		{errors.Join(protocol.ErrIncompleteResponse, errors.New("save failed")), "error_during_execution"},
	} {
		h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "", tc.err }, Options{})
		h.send(t, user("task", "review"))
		if frame := h.until(t, "result"); frame["subtype"] != tc.subtype {
			t.Fatal(frame)
		}
		h.finish(t)
	}
}
