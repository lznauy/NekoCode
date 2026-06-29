package gate

import "testing"

func TestResponseGateRetryLimit(t *testing.T) {
	g := &ResponseGate{MaxRetries: 2}

	if retry, hint := g.TryRetry("blocked"); !retry || hint != "blocked" {
		t.Fatalf("first retry = (%v, %q), want true blocked", retry, hint)
	}
	if retry, _ := g.TryRetry("blocked"); !retry {
		t.Fatal("second retry should be allowed")
	}
	if retry, hint := g.TryRetry("blocked"); retry || hint != "" {
		t.Fatalf("third retry = (%v, %q), want exhausted", retry, hint)
	}
}

func TestResponseGateReset(t *testing.T) {
	g := NewResponseGate()
	g.TryRetry("blocked")  // retries = 1
	g.TryRetry("blocked")  // retries = 2, exhausted
	g.Reset()
	// After reset, TryRetry should allow again as if fresh
	if retry, _ := g.TryRetry("blocked"); !retry {
		t.Fatal("after reset, first TryRetry should succeed")
	}
}
