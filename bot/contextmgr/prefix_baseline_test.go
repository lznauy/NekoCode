package contextmgr

import (
	"testing"

	"nekocode/bot/provider/types"
)

// A resumed session must report why its first request missed instead of a bare
// cold-start: the persisted baseline carries the previous request's head.
func TestPrefixBaselineRoundTrip(t *testing.T) {
	request := ModelRequest{Tools: []types.ToolDef{{Type: "function", Function: types.FunctionDef{Name: "read"}}}}

	first := New(Config{SystemPrompt: "stable prompt"})
	first.Add("user", "hello")
	first.BuildRequest(request)

	baseline := first.Snapshot().PrefixBaseline
	if baseline.System == "" || baseline.Tools == "" {
		t.Fatalf("baseline not captured: %+v", baseline)
	}

	history := first.Snapshot().Messages

	// The head is reproduced byte for byte, so a resumed call reports no
	// change: any miss can only have come from the provider's tail or its
	// cache TTL, never from our prefix.
	same := New(Config{SystemPrompt: "stable prompt"})
	same.Restore(ManagerSnapshot{SystemPrompt: "stable prompt", Messages: history, PrefixBaseline: baseline})
	same.BuildRequest(request)
	if parts := same.state.prefix.pending; len(parts) != 0 {
		t.Fatalf("identical head reported as changed: %v", parts)
	}

	// A different head is a real epoch and must be attributed as one.
	changed := New(Config{SystemPrompt: "different prompt"})
	changed.Restore(ManagerSnapshot{SystemPrompt: "different prompt", Messages: history, PrefixBaseline: baseline})
	changed.BuildRequest(request)
	if parts := changed.state.prefix.pending; len(parts) != 1 || parts[0] != "system" {
		t.Fatalf("changed head parts = %v, want [system]", parts)
	}
}

// Without a usable baseline (fresh session, or a session saved before this was
// recorded) the first call stays a cold start.
func TestPrefixBaselineAbsentStaysColdStart(t *testing.T) {
	m := New(Config{SystemPrompt: "prompt"})
	m.BuildRequest(ModelRequest{})
	if parts := m.state.prefix.pending; len(parts) != 1 || parts[0] != "cold-start" {
		t.Fatalf("first call parts = %v, want [cold-start]", parts)
	}

	restored := New(Config{SystemPrompt: "prompt"})
	restored.Restore(ManagerSnapshot{SystemPrompt: "prompt"})
	restored.BuildRequest(ModelRequest{})
	if parts := restored.state.prefix.pending; len(parts) != 1 || parts[0] != "cold-start" {
		t.Fatalf("restored without baseline parts = %v, want [cold-start]", parts)
	}
}

// A malformed persisted digest must not invent a baseline.
func TestPrefixBaselineRejectsMalformedDigest(t *testing.T) {
	m := New(Config{SystemPrompt: "prompt"})
	m.Restore(ManagerSnapshot{
		SystemPrompt:   "prompt",
		PrefixBaseline: PrefixBaseline{System: "not-hex", Tools: "00ff"},
	})
	m.BuildRequest(ModelRequest{})
	if parts := m.state.prefix.pending; len(parts) != 1 || parts[0] != "cold-start" {
		t.Fatalf("malformed baseline parts = %v, want [cold-start]", parts)
	}
}
