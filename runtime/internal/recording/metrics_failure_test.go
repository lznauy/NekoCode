package recording

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func TestAcceptedPendingMetricsSurviveFailedIntervalWrite(t *testing.T) {
	// /dev/full provides deterministic ENOSPC without filling the filesystem.
	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skip("requires /dev/full")
	}
	defer full.Close()
	r, err := NewEventRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	metric := func(seq uint64) core.Event {
		return core.Event{RunID: "run_1", Sequence: seq, Time: time.Unix(int64(seq), 0), Type: core.EventMetricsUpdated, Payload: protocol.Metrics{CompletionTokens: int(seq)}}
	}
	for _, seq := range []uint64{1, 3} {
		if err := r.RecordError(metric(seq)); err != nil {
			t.Fatal(err)
		}
	}
	run := r.runs["run_1"]
	path := filepath.Join(run.dir, "events.jsonl")
	good := run.events
	run.events = full
	run.lastMetrics = time.Now().Add(-2 * time.Second)
	err = r.RecordError(metric(2))
	run.events = good
	if err == nil {
		t.Fatal("expected injected ENOSPC")
	}
	if err := r.RecordError(core.Event{RunID: "run_1", Sequence: 4, Time: time.Unix(4, 0), Type: core.EventRunDone, Payload: core.RunResult{}}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close() // The earlier error is deliberately retained for reporting.
	events, err := loadRecordedEventFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Type == core.EventMetricsUpdated && ev.Sequence == 3 {
			return
		}
	}
	t.Fatal("previously accepted pending metrics (sequence 3) lost after transient write failure")
}
