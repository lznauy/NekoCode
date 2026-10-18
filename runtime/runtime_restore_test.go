package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"nekocode/runtime/internal/recording"
)

// TestEventRecordingRestoresTailInBackground verifies the split restore: the
// newest runs are available in the run store immediately, and the long tail
// (beyond immediateRestoredRuns) streams in from the background goroutine
// without blocking EnableEventRecording's return.
func TestEventRecordingRestoresTailInBackground(t *testing.T) {
	baseDir := t.TempDir()

	// Record more runs than the synchronous slice covers.
	const totalRuns = immediateRestoredRuns + 5
	for i := 1; i <= totalRuns; i++ {
		rec, err := recording.NewEventRecorder(baseDir)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		rec.Record(Event{
			ID: "evt_a", RunID: RunID("run_" + itoa(i)), Type: EventInputAccepted, Time: now,
			Source: SourceRef{Kind: "test"},
			Payload: MessagePayload{
				Content: "input " + itoa(i), Source: SourceRef{Kind: "test"},
			},
		})
		rec.Record(Event{
			ID: "evt_b", RunID: RunID("run_" + itoa(i)), Type: EventRunDone, Time: now.Add(time.Millisecond),
			Payload: RunResult{Output: "output " + itoa(i)},
		})
		_ = rec.Close()
	}

	rt := newTestRuntime(&testBot{})
	start := time.Now()
	if err := rt.EnableEventRecording(baseDir); err != nil {
		t.Fatalf("EnableEventRecording: %v", err)
	}
	// The synchronous phase decodes only immediateRestoredRuns files; on
	// this tiny fixture it is fast regardless, so the meaningful assertion is
	// the end state, polled below.

	// The newest run is present right away.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := rt.LookupRun(RunID("run_" + itoa(totalRuns))); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("newest run not restored synchronously")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The background tail eventually restores the oldest run too.
	for {
		if _, ok := rt.LookupRun(RunID("run_1")); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tail run never restored by the background loader")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The immediate batch was already Recorded synchronously; the background
	// restore must not Record it a second time (runstore accumulates without
	// dedup — event counts and outputs would double).
	newest, ok := rt.LookupRun(RunID("run_" + itoa(totalRuns)))
	if !ok {
		t.Fatal("newest run missing after restore")
	}
	if newest.EventCount != 2 {
		t.Fatalf("newest run event count = %d, want 2 (input_accepted + run_done, no double record)", newest.EventCount)
	}

	// The run sequence advanced past the restored maximum.
	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "run_" + itoa(totalRuns+1) + "_"; !strings.HasPrefix(string(runID), want) {
		t.Fatalf("run id = %q, want %q", runID, want)
	}
	waitForRun(t, rt, runID)
	_ = start
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
