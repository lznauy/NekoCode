package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"nekocode/runtime/internal/recording"
)

func TestReplayArchivedOversizedEvent(t *testing.T) {
	base := t.TempDir()
	rec, err := recording.NewEventRecorder(base)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record(Event{ID: "evt_1", Sequence: 1, RunID: "run_7", Type: EventRunDone, Time: time.Now(), Payload: RunResult{Output: strings.Repeat("x", 9<<20)}})
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
	ch, err := rt.ReplayEvents(ctx, EventFilter{RunID: "run_7"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev, ok := <-ch:
		if !ok || ev.Sequence != 1 {
			t.Fatalf("oversized archived event missing: %+v, open=%v", ev, ok)
		}
		result, ok := ev.Payload.(RunResult)
		if !ok || len(result.Output) != 9<<20 {
			t.Fatal("archived terminal output was truncated")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("oversized archived event was dropped during replay")
	}
	if len(rt.events.History(EventFilter{RunID: "run_7"})) != 0 {
		t.Fatal("subscriber replay polluted the bounded shared history")
	}
}
