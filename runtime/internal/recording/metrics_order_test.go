package recording

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func TestMetricsCoalescingRetainsNewestTimestamp(t *testing.T) {
	for _, crossInterval := range []bool{false, true} {
		t.Run(fmt.Sprint("cross_interval_", crossInterval), func(t *testing.T) {
			r, err := NewEventRecorder(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			for _, seq := range []uint64{1, 3, 2} {
				if seq == 2 && crossInterval {
					r.runs["run_1"].lastMetrics = time.Now().Add(-2 * time.Second)
				}
				if err := r.RecordError(core.Event{RunID: "run_1", Sequence: seq, Time: time.Unix(int64(seq), 0), Type: core.EventMetricsUpdated, Payload: protocol.Metrics{CompletionTokens: int(seq)}}); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(r.RunDir("run_1"), "events.jsonl")
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			events := LoadRecordedEventFiles([]RecordedRun{{Path: path}})
			if len(events) == 0 {
				t.Fatal("no recorded events")
			}
			if got := events[len(events)-1].Payload.(protocol.Metrics).CompletionTokens; got != 3 {
				t.Fatalf("latest restored metrics = %d; want 3", got)
			}
		})
	}
}
