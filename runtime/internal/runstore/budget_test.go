package runstore

import (
	"nekocode/runtime/internal/core"
	"strings"
	"testing"
)

func TestRestoredCacheBudgetAndCurrentIdentity(t *testing.T) {
	s := NewRunStore(100)
	s.Record(core.Event{RunID: "live", Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: "current"}})
	for _, id := range []core.RunID{"old1", "old2", "old3"} {
		s.CacheRestored(core.RunSnapshot{ID: id, Status: core.RunDone, Output: strings.Repeat("x", 4<<20)})
	}
	total := 0
	for _, r := range s.runs {
		total += snapshotBytes(r.snapshot)
	}
	if total > DefaultBytes {
		t.Fatalf("cache exceeded budget: %d", total)
	}
	current, ok := s.Current()
	if !ok || current.ID != "live" {
		t.Fatal("archive changed current run")
	}
	s.CacheRestored(core.RunSnapshot{ID: "interrupted", Status: core.RunRunning, Output: "old"})
	if _, ok := s.Lookup("interrupted"); ok {
		t.Fatal("interrupted archive pinned in live cache")
	}
}
