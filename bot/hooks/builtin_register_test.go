package hooks

import "testing"

func TestRegisterBuiltinRegistersExpectedHookSet(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltin(r)

	hooks := r.List()
	if len(hooks) != 9 {
		t.Fatalf("builtin hooks = %d, want 9", len(hooks))
	}
	want := map[string]bool{
		"quota":                   true,
		"verification":            true,
		"exploration_exhausted":   true,
		"exploration_guard":       true,
		"explore_cascade":         true,
		"progress_stall":          true,
		"completion_quality":      true,
		"garbled_circuit_breaker": true,
		"final_check":             true,
	}
	for _, h := range hooks {
		if !want[h.Name] {
			t.Fatalf("unexpected builtin hook %q", h.Name)
		}
	}
}
