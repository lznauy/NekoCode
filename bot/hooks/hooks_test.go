package hooks

import (
	"strings"
	"testing"
)

func TestEmptyManager(t *testing.T) {
	m := NewManager()
	if len(m.Evaluate(PointPreTurn)) != 0 {
		t.Error("empty manager should produce no results")
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
	hk := QuotaHook()

	// Silent when quota not hard.
	snap := makeSnap()
	if r := hk.On(snap); r != nil {
		t.Error("quota not hard -> silent")
	}

	// Fire when hard.
	snap = makeSnap()
	snap.store.SetGauge(KeyQuotaHard, 1)
	snap.store.SetGauge(KeyQuotaReads, 3)
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("quota hard -> fire")
	}
	if r.Hint.Severity != "warning" {
		t.Errorf("expected warning, got %s", r.Hint.Severity)
	}

	// Critical at 1.
	snap = makeSnap()
	snap.store.SetGauge(KeyQuotaHard, 1)
	snap.store.SetGauge(KeyQuotaReads, 1)
	r = hk.On(snap)
	if r == nil || r.Hint == nil || r.Hint.Severity != "critical" {
		t.Error("reads=1 -> critical")
	}
}

func TestVerificationHook(t *testing.T) {
	hk := VerificationHook()

	// No file modified -> silent.
	snap := makeSnap()
	if r := hk.On(snap); r != nil {
		t.Error("no file modified -> silent")
	}

	// File modified -> fire once.
	snap = makeSnap()
	snap.store.SetFlag(KeyFileModified, true)
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("file modified -> fire")
	}

	// Second call -> silent (already injected).
	r = hk.On(snap)
	if r != nil {
		t.Error("already injected -> silent")
	}

	// Flag cleared (unset) -> reset state, then fire again.
	snap = makeSnap()
	hk.On(snap)       // reset injected
	snap.store.SetFlag(KeyFileModified, true)
	r = hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Error("after reset -> should fire again")
	}
}

func TestGarbledCircuitBreaker(t *testing.T) {
	hk := GarbledCircuitBreaker()
	snap := makeSnap()
	if r := hk.On(snap); r != nil {
		t.Error("no garbled -> silent")
	}
	snap.store.IncCounter(KeyRespGarbled)
	snap.store.IncCounter(KeyRespGarbled)
	if r := hk.On(snap); r != nil {
		t.Error("garbled=2 -> silent")
	}
	snap.store.IncCounter(KeyRespGarbled)
	r := hk.On(snap)
	if r == nil || r.Stop == nil || *r.Stop != StopFormatError {
		t.Error("garbled=3 -> stop")
	}
}

func makeSnap() *Snapshot {
	return &Snapshot{store: &Store{}}
}
