package contextrefs

import (
	"context"
	"math"
	"testing"

	"nekocode/bot/extension/tool/runtime/refbridge"
)

func TestToolValidatesQueryWithoutAcceptingCallerPathsOrScope(t *testing.T) {
	tool := New()
	calls := 0
	tool.Wire(func(_ context.Context, q refbridge.Query) (refbridge.Page, error) {
		calls++
		if q.Limit != 20 || q.Before != 7 || q.ID != "" {
			t.Fatalf("wrong query: %+v", q)
		}
		return refbridge.Page{SessionID: "host-session"}, nil
	})
	if _, err := tool.Execute(t.Context(), map[string]any{"action": "list", "before": float64(7), "path": "/private", "session_id": "other"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{
		{"action": "list", "limit": float64(21)}, {"action": "list", "limit": float64(0)}, {"action": "list", "before": float64(-1)}, {"action": "list", "before": 1.5}, {"action": "list", "before": math.NaN()}, {"action": "list", "before": math.Inf(1)}, {"action": "list", "before": 1e100}, {"action": "inspect"}, {"action": "inspect", "id": string(make([]byte, 129))}, {"action": "unknown"},
	} {
		if _, err := tool.Execute(t.Context(), args); err == nil {
			t.Fatalf("accepted malformed query: %+v", args)
		}
	}
	if calls != 1 {
		t.Fatal("invalid query reached reader")
	}
	if _, err := New().Execute(t.Context(), map[string]any{"action": "list"}); err == nil {
		t.Fatal("unwired reader succeeded")
	}
}
