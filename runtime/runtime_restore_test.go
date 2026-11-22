package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekocode/runtime/internal/recording"
)

// TestEventRecordingRestoresOlderRunsOnlyOnDemand verifies that startup keeps
// older run bodies on disk, while explicit lookup restores them without moving
// the current run identity.
func TestEventRecordingRestoresOlderRunsOnlyOnDemand(t *testing.T) {
	baseDir := t.TempDir()

	// Record more runs than the synchronous slice covers.
	const totalRuns = 6
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
	if err := rt.EnableEventRecording(baseDir); err != nil {
		t.Fatalf("EnableEventRecording: %v", err)
	}
	defer rt.Close()
	newestID := RunID("run_" + itoa(totalRuns))
	if _, ok := rt.runs.Lookup(newestID); !ok {
		t.Fatal("newest run not restored synchronously")
	}
	if _, ok := rt.runs.Lookup("run_1"); ok {
		t.Fatal("startup eagerly restored older run body")
	}
	oldest, ok := rt.LookupRun("run_1")
	if !ok || oldest.Output != "output 1" || oldest.EventCount != 2 {
		t.Fatalf("on-demand oldest run = %#v, ok=%v", oldest, ok)
	}
	current, ok := rt.CurrentRun()
	if !ok || current.ID != newestID {
		t.Fatalf("archive lookup moved current run: %q, want %q", current.ID, newestID)
	}
	// Neither lookup nor cache population may re-record the newest events.
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
}

func TestStartupRestoresLatestRetainableRun(t *testing.T) {
	root := t.TempDir()
	rec, err := recording.NewEventRecorder(root)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record(Event{RunID: "run_1", Type: EventRunDone, Payload: RunResult{Output: "previous"}})
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "session", "runs", "run_2")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, recording.RetentionBytes+1); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	rt := newTestRuntime(&testBot{})
	defer rt.Close()
	if err := rt.EnableEventRecording(root); err != nil {
		t.Fatal(err)
	}
	if run, ok := rt.CurrentRun(); !ok || run.ID != "run_1" || run.Output != "previous" {
		t.Fatalf("current run = %+v, ok=%v", run, ok)
	}
	if rt.nextRun != 2 {
		t.Fatalf("run sequence = %d, want 2", rt.nextRun)
	}
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

func TestRestoredObservedRunUsesArchiveClock(t *testing.T) {
	start := time.Date(2020, 1, 2, 10, 0, 0, 0, time.UTC)
	for _, withStart := range []bool{true, false} {
		baseDir := t.TempDir()
		rec, err := recording.NewEventRecorder(baseDir)
		if err != nil {
			t.Fatal(err)
		}
		if withStart {
			rec.Record(Event{RunID: "run_1", Type: EventRunStarted, Time: start})
		}
		end := start.Add(2 * time.Minute)
		rec.Record(Event{RunID: "run_1", Type: EventRunDone, Time: end, Payload: RunResult{Output: "done"}})
		if err := rec.Close(); err != nil {
			t.Fatal(err)
		}
		rt := newTestRuntime(&testBot{})
		if err := rt.EnableEventRecording(baseDir); err != nil {
			t.Fatal(err)
		}
		r, ok := rt.LookupRun("run_1")
		if !ok {
			t.Fatal("missing restored run")
		}
		got := SummarizeObservedRun(r)
		wantStart := end
		if withStart {
			wantStart = start
		}
		rt.Close()
		if !got.StartedAt.Equal(wantStart) || got.FinishedAt == nil || !got.FinishedAt.Equal(end) || got.DurationMS != end.Sub(wantStart).Milliseconds() {
			t.Fatalf("start event=%t: %+v", withStart, got)
		}
	}
}
