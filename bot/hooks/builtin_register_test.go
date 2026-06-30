package hooks

import "testing"

func TestRegisterBuiltinRegistersExpectedHookSet(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltin(r)

	hooks := r.List()
	if len(hooks) != 12 {
		t.Fatalf("builtin hooks = %d, want 12", len(hooks))
	}
	want := map[string]bool{
		"quota":                   true,
		"tool_result_guardrail":   true,
		"read_before_write":       true,
		"read_only_spiral":        true,
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
