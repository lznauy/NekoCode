package runtime

import (
	"context"
	"nekocode/runtime/internal/recording"
	"os"
	"testing"
	"time"
)

func TestReplayCaughtUpCursorDoesNotDuplicateCachedArchive(t *testing.T) {
	base := t.TempDir()
	rec, err := recording.NewEventRecorder(base)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record(Event{ID: "evt_1", RunID: "run_7", Type: EventRunDone, Time: time.Now(), Payload: RunResult{Output: "done"}})
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	rt := newTestRuntime(&testBot{})
	if err := rt.EnableEventRecording(base); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := rt.ReplayEvents(ctx, EventFilter{RunID: "run_7", After: 1}); err != nil {
		t.Fatal(err)
	}
	history := rt.events.History(EventFilter{RunID: "run_7"})
	if len(history) != 1 {
		t.Fatalf("caught-up reconnect duplicated archive: got %d events, want 1", len(history))
	}
}

func TestReplayCachedRunSurvivesMissingArchive(t *testing.T) {
	base := t.TempDir()
	rec, err := recording.NewEventRecorder(base)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record(Event{ID: "evt_1", Sequence: 1, RunID: "run_7", Type: EventRunDone, Time: time.Now(), Payload: RunResult{Output: "cached"}})
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	rt := newTestRuntime(&testBot{})
	if err := rt.EnableEventRecording(base); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	rt.mu.Lock()
	archive := rt.archivedRuns["run_7"].Path
	rt.mu.Unlock()
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, err := rt.ReplayEvents(ctx, EventFilter{RunID: "run_7"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev, ok := <-ch:
		if !ok || ev.Type != EventRunDone {
			t.Fatal("cached completion unavailable")
		}
	case <-ctx.Done():
		t.Fatal("cached replay timed out")
	}
}
