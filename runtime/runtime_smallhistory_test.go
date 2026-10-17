package runtime

import (
	"context"
	"testing"
	"time"

	"nekocode/runtime/internal/recording"
)

// TestEventRecordingImportsSmallHistorySynchronously guards the split-restore
// edge case: when the whole history fits in the immediate slice (no tail),
// the event-bus history must still be imported — the background goroutine
// never runs in that branch.
func TestEventRecordingImportsSmallHistorySynchronously(t *testing.T) {
	baseDir := t.TempDir()
	rec, err := recording.NewEventRecorder(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record(Event{
		ID: "evt_1", RunID: "run_7", Type: EventRunDone, Time: time.Now(),
		Payload: RunResult{Output: "small history output"},
	})
	_ = rec.Close()

	rt := newTestRuntime(&testBot{})
	if err := rt.EnableEventRecording(baseDir); err != nil {
		t.Fatalf("EnableEventRecording: %v", err)
	}

	// The run store answers immediately…
	snapshot, ok := rt.LookupRun("run_7")
	if !ok || snapshot.Output != "small history output" {
		t.Fatalf("runstore = %#v ok=%v", snapshot, ok)
	}
	// …and so does the event-bus history, without waiting for any tail.
	// The replay channel stays open for live events, so bound the read with
	// a timeout and drain until the restored event shows up.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events, err := rt.ReplayEvents(ctx, EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for ev := range events {
		if ev.RunID == "run_7" {
			found = true
			break
		}
	}
	cancel()
	if !found {
		t.Fatal("small history was never imported into the event bus")
	}
}
