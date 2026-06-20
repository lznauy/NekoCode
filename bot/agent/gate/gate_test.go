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
	g.TryRetry("blocked")
	g.Reset()
	if g.Retries() != 0 {
		t.Fatalf("retries = %d, want 0", g.Retries())
	}
}
