package recording

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func TestMetricsCoalescingPreservesFinalStateAndOrdering(t *testing.T) {
	for _, end := range []string{"done", "failed", "cancelled", "close", "end_run"} {
		t.Run(end, func(t *testing.T) {
			r, err := NewEventRecorder(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			emit := func(seq uint64, typ core.EventType) {
				ev := core.Event{RunID: "run_1", Sequence: seq, ID: fmt.Sprint(seq), Time: time.Unix(0, int64(seq)), Type: typ}
				if typ == core.EventMetricsUpdated {
					ev.Payload = protocol.Metrics{CompletionTokens: int(seq), Duration: fmt.Sprint(seq)}
				} else if typ != core.EventRunStarted {
					ev.Payload = core.RunResult{Output: "done"}
				}
				if err := r.RecordError(ev); err != nil {
					t.Fatal(err)
				}
			}
			emit(1, core.EventMetricsUpdated)
			emit(2, core.EventMetricsUpdated)
			emit(3, core.EventMetricsUpdated)
			// A non-metric frame must flush pending metrics first.
			emit(4, core.EventRunStarted)
			emit(5, core.EventMetricsUpdated)
			emit(6, core.EventMetricsUpdated)
			path := filepath.Join(r.RunDir("run_1"), "events.jsonl")
			switch end {
			case "done":
				emit(7, core.EventRunDone)
			case "failed":
				emit(7, core.EventRunFailed)
			case "cancelled":
				emit(7, core.EventRunCancelled)
			case "close":
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			case "end_run":
				r.EndRun("run_1")
			}
			events, err := loadRecordedEventFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := []uint64{1, 3, 4, 6}
			if end == "done" || end == "failed" || end == "cancelled" {
				want = append(want, 7)
			}
			if len(events) != len(want) {
				t.Fatalf("got %d events want %d", len(events), len(want))
			}
			for i, ev := range events {
				if ev.Sequence != want[i] {
					t.Fatalf("sequence %d want %d", ev.Sequence, want[i])
				}
			}
			// Recorded payloads retain the latest usage, including the final duration.
			latest := events[3].Payload.(protocol.Metrics)
			if latest.CompletionTokens != 6 || latest.Duration != "6" {
				t.Fatalf("latest metrics: %+v", latest)
			}
		})
	}
}

func TestMetricsHeartbeatPersistsWithoutOtherEvents(t *testing.T) {
	r, err := NewEventRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ev := core.Event{RunID: "run_1", Type: core.EventMetricsUpdated, Payload: protocol.Metrics{CompletionTokens: 1}}
	if err := r.RecordError(ev); err != nil {
		t.Fatal(err)
	}
	r.runs[ev.RunID].lastMetrics = time.Now().Add(-2 * time.Second)
	ev.Payload = protocol.Metrics{CompletionTokens: 2}
	if err := r.RecordError(ev); err != nil {
		t.Fatal(err)
	}
	events, err := loadRecordedEventFile(filepath.Join(r.RunDir(ev.RunID), "events.jsonl"))
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
}

func TestMetricsCadenceReducesRecordedSamples(t *testing.T) {
	r, err := NewEventRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i := 0; i < 100; i++ {
		// Simulate 100 samples at 10 Hz without sleeping in the test.
		if i > 0 && i%10 == 0 {
			r.runs["run_1"].lastMetrics = time.Now().Add(-time.Second)
		}
		if err := r.RecordError(core.Event{RunID: "run_1", Type: core.EventMetricsUpdated, Sequence: uint64(i + 1), Payload: protocol.Metrics{CompletionTokens: i}}); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(r.RunDir("run_1"), "events.jsonl")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	events, err := loadRecordedEventFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 11 {
		t.Fatalf("recorded=%d want 11 from 100 samples including final flush", len(events))
	}
	if got := events[len(events)-1].Payload.(protocol.Metrics).CompletionTokens; got != 99 {
		t.Fatalf("final usage=%d want99", got)
	}
}
