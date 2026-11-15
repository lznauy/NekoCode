package headless

import (
	"context"
	rt "nekocode/runtime"
	"nekocode/util/modelbudget"
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
	if f := rpc(t, h, "initialize", map[string]any{"model_call_budget": 100}); f["subtype"] != "success" {
		t.Fatal(f)
	}
	info := rpc(t, h, "server.info", nil)["response"].(map[string]any)
	if info["execution_budget"].(map[string]any)["limit"] != float64(100) {
		t.Fatal(info)
	}
	h.send(t, user("task", "review"))
	h.until(t, "result")
	if got := <-seen; got != 100 {
		t.Fatal(got)
	}
	h.finish(t)
}
