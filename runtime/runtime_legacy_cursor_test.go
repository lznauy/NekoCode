package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nekocode/runtime/internal/recording"
)

func TestArchivedReplayAfterNormalizesLegacySequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data := "{\"id\":\"evt_1\",\"run_id\":\"legacy\",\"type\":\"run_started\"}\n{\"id\":\"evt_2\",\"run_id\":\"legacy\",\"type\":\"run_done\",\"payload\":{\"output\":\"done\"}}\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	rt := newTestRuntime(&testBot{})
	defer rt.Close()
	rt.archivedRuns = map[RunID]recording.RecordedRun{"legacy": {Path: path}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	events, err := rt.ReplayEvents(ctx, EventFilter{RunID: "legacy", After: 1})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev, ok := <-events:
		if !ok || ev.Sequence != 2 || ev.ID != "evt_2" {
			t.Fatalf("replayed event=%+v, open=%v", ev, ok)
		}
	case <-ctx.Done():
		t.Fatal("legacy event after cursor was silently filtered out")
	}
	if history := rt.events.History(EventFilter{RunID: "legacy"}); len(history) != 0 {
		t.Fatal("archive replay populated shared history")
	}
}
