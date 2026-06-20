package hooks

import "testing"

func TestRegistryCountsAndGovernanceStatsReset(t *testing.T) {
	r := NewRegistry()
	r.Register(Hook{
		Name:  "hint",
		Point: PreTurn,
		On: func(s *Snapshot) *Result {
			return &Result{Hint: &Hint{Type: "test", Content: "ok"}}
		},
	})

	results := r.Evaluate(PreTurn, "", false)
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	counts := r.HookCountsSnapshot()
	if counts.Evaluations != 1 || counts.Hints != 1 {
		t.Fatalf("counts = %+v, want one evaluation and hint", counts)
	}

	_ = r.GovernanceStats()
	counts = r.HookCountsSnapshot()
	if counts != (HookCounts{}) {
		t.Fatalf("counts after GovernanceStats = %+v, want zero", counts)
	}
}

func TestEmptyRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.List()) != 0 {
		t.Error("expected empty hook list")
	}
}

func TestRegistryStatePatchWritesBackPolicyKeys(t *testing.T) {
	r := NewRegistry()
	r.Register(Hook{
		Name:  "patch",
		Point: PreTurn,
		On: func(s *Snapshot) *Result {
			return &Result{StatePatch: &StatePatch{
				Ints:    map[string]int64{"policy:block": 1},
				Strings: map[string]string{"policy:reason": "blocked"},
			}}
		},
	})

	r.Evaluate(PreTurn, "", false)
	if got := r.store["policy:block"]; got != 1 {
		t.Fatalf("policy int patch = %d, want 1", got)
	}
	if got := r.strVals["policy:reason"]; got != "blocked" {
		t.Fatalf("policy string patch = %q, want blocked", got)
	}
}

func TestResetSession(t *testing.T) {
	r := NewRegistry()
	r.Set(StoreLedgerModified, 3)
	r.Set(StoreQuotaReads, 5)

	r.ResetSession()
	if r.store[StoreLedgerModified] != 0 || r.store[StoreQuotaReads] != 0 {
		t.Error("store should be empty after session reset")
	}
}

func TestRegistryUnregisterWhere(t *testing.T) {
	r := NewRegistry()
	r.Register(Hook{Name: "keep", Point: PreTurn})
	r.Register(Hook{Name: "drop", Point: PreTurn})

	r.UnregisterWhere(func(h Hook) bool { return h.Name == "drop" })
	hooks := r.List()
	if len(hooks) != 1 || hooks[0].Name != "keep" {
		t.Fatalf("hooks = %+v, want only keep", hooks)
	}
}
