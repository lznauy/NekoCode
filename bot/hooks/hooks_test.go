package hooks

import (
	"strings"
	"testing"
)

func TestEmptyRegistry(t *testing.T) {
	r := NewRegistry()
	if len(r.Evaluate(PreTurn, "", false)) != 0 {
		t.Error("empty registry should produce no results")
	}
	if FormatHints(nil) != "" || FormatHints([]Hint{}) != "" {
		t.Error("expected empty for nil/empty")
	}
}

func TestFormatHints(t *testing.T) {
	result := FormatHints([]Hint{{Type: "quota", Severity: "warning", Content: "3 reads left"}})
	if !strings.Contains(result, "<hints>") || !strings.Contains(result, `type="quota"`) {
		t.Errorf("bad format: %s", result)
	}
}

func TestQuotaHook(t *testing.T) {
	hk := quotaHook()
	snap := &Snapshot{Store: make(map[string]int64)}

	snap.set(StoreQuotaReads, 5)
	if r := hk.On(snap); r != nil {
		t.Error("reads=5 -> silent")
	}

	snap.set(StoreQuotaReads, 4)
	if r := hk.On(snap); r != nil {
		t.Error("reads=4 -> silent")
	}

	snap.set(StoreQuotaReads, 2)
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("reads=2 -> fire")
	}
	if r.Hint.Severity != "warning" {
		t.Errorf("expected warning, got %s", r.Hint.Severity)
	}

	snap.set(StoreQuotaReads, 0)
	r = hk.On(snap)
	if r == nil || r.Hint == nil || r.Hint.Severity != "critical" {
		t.Error("reads=0 -> critical")
	}
}

func TestVerificationHook(t *testing.T) {
	hk := verificationHook()
	snap := &Snapshot{Store: make(map[string]int64)}

	if r := hk.On(snap); r != nil {
		t.Error("no file modified -> silent")
	}

	snap.set(StoreFileModified, 1)
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("file modified -> fire")
	}

	r = hk.On(snap)
	if r != nil {
		t.Error("already injected -> silent")
	}
}

func TestGarbledCircuitBreaker(t *testing.T) {
	hk := garbledCircuitBreaker()
	snap := &Snapshot{Store: make(map[string]int64)}

	if r := hk.On(snap); r != nil {
		t.Error("no garbled -> silent")
	}
	snap.set(StoreRespGarbled, 2)
	if r := hk.On(snap); r != nil {
		t.Error("garbled=2 -> silent")
	}
	snap.set(StoreRespGarbled, 3)
	r := hk.On(snap)
	if r == nil || r.Stop == nil || *r.Stop != StopFormatError {
		t.Error("garbled=3 -> stop")
	}
}

func TestResetSession(t *testing.T) {
	r := NewRegistry()
	r.Set(StoreFileModified, 1)
	r.Set(StoreQuotaReads, 1)
	r.ResetSession()

	if r.store[StoreFileModified] != 0 || r.store[StoreQuotaReads] != 0 {
		t.Error("store should be empty after session reset")
	}
}
