package runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCompletedRunEvictedByBytesRemainsReadable(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	if err := rt.EnableEventRecording(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	for i := 1; i <= 4; i++ {
		id := RunID(fmt.Sprintf("run_%d_test", i))
		if _, err := rt.beginRecordedRun(id); err != nil {
			t.Fatal(err)
		}
		ev := Event{RunID: id, Type: EventRunDone, Time: time.Now(), Payload: RunResult{Output: strings.Repeat("x", 3<<20)}}
		if err := rt.recorder.RecordError(ev); err != nil {
			t.Fatal(err)
		}
		rt.runs.Record(ev)
		rt.releaseRecordedRun(id)
	}
	if _, ok := rt.runs.Lookup("run_1_test"); ok {
		t.Fatal("byte budget did not evict old run")
	}
	snapshot, ok := rt.LookupRun("run_1_test")
	if !ok || len(snapshot.Output) != 3<<20 {
		t.Fatal("evicted run was not restored from its recorded path")
	}
	current, ok := rt.CurrentRun()
	if !ok || current.ID != "run_4_test" {
		t.Fatal("archive lookup changed current run")
	}
}

func TestRunListKeepsLiveRunAfterArchiveLookup(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	defer rt.Close()
	now := time.Now()
	rt.runs.Record(Event{RunID: "live", Type: EventRunStarted, Time: now})
	rt.runs.CacheRestored(RunSnapshot{ID: "old", Status: RunDone, UpdatedAt: now.Add(-time.Hour)})
	runs := rt.Runs(1)
	if len(runs) != 1 || runs[0].ID != "live" {
		t.Fatalf("archive lookup displaced live run from latest list: %+v", runs)
	}
}

func TestLargeSingleEventEvictedRunRemainsReadable(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	if err := rt.EnableEventRecording(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	for i := 1; i <= 2; i++ {
		id := RunID(fmt.Sprintf("run_%d_large", i))
		if _, err := rt.beginRecordedRun(id); err != nil {
			t.Fatal(err)
		}
		ev := Event{RunID: id, Type: EventRunDone, Time: time.Now(), Payload: RunResult{Output: strings.Repeat("x", 5<<20)}}
		if err := rt.recorder.RecordError(ev); err != nil {
			t.Fatal(err)
		}
		rt.runs.Record(ev)
		rt.releaseRecordedRun(id)
	}
	if _, ok := rt.runs.Lookup("run_1_large"); ok {
		t.Fatal("old run was not evicted")
	}
	snap, ok := rt.LookupRun("run_1_large")
	if !ok || len(snap.Output) != 5<<20 {
		t.Fatal("large event could not be restored after byte-budget eviction")
	}
}
