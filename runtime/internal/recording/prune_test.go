package recording

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nekocode/runtime/internal/core"
)

func coreEvent(runID string) core.Event {
	return core.Event{RunID: core.RunID(runID), Type: core.EventRunStarted}
}

// writeBatch creates a batch directory containing one run event file and
// stamps both the file and the batch directory with the given time.
func writeBatch(t *testing.T, baseDir, name string, size int, mod time.Time) {
	t.Helper()
	runDir := filepath.Join(baseDir, name, "run_1")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	if err := os.WriteFile(filepath.Join(runDir, "events.jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	batchDir := filepath.Join(baseDir, name)
	if err := os.Chtimes(batchDir, mod, mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(runDir, "events.jsonl"), mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestPruneRecordedRunsRemovesStaleBatches(t *testing.T) {
	baseDir := t.TempDir()
	now := time.Now()
	writeBatch(t, baseDir, "old", 100, now.Add(-40*24*time.Hour))
	writeBatch(t, baseDir, "fresh", 100, now)

	if err := PruneRecordedRuns(baseDir, 30*24*time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "old")); !os.IsNotExist(err) {
		t.Fatalf("stale batch survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "fresh")); err != nil {
		t.Fatalf("fresh batch was pruned: %v", err)
	}
}

func TestPruneRecordedRunsEnforcesSizeCap(t *testing.T) {
	baseDir := t.TempDir()
	now := time.Now()
	// All batches are young; only the size cap should drive pruning, oldest
	// first. 1800 bytes against a 1000-byte cap needs two removals.
	writeBatch(t, baseDir, "b_youngest", 600, now)
	writeBatch(t, baseDir, "b_middle", 600, now.Add(-time.Hour))
	writeBatch(t, baseDir, "b_oldest", 600, now.Add(-2*time.Hour))

	if err := PruneRecordedRuns(baseDir, 30*24*time.Hour, 1000); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"b_oldest", "b_middle"} {
		if _, err := os.Stat(filepath.Join(baseDir, removed)); !os.IsNotExist(err) {
			t.Fatalf("batch %s must be pruned under the size cap", removed)
		}
	}
	if _, err := os.Stat(filepath.Join(baseDir, "b_youngest")); err != nil {
		t.Fatalf("newest batch must survive: %v", err)
	}
}

func TestPruneRecordedRunsKeepsActiveBatch(t *testing.T) {
	baseDir := t.TempDir()
	// An active recorder has a just-created batch directory.
	rec, err := NewEventRecorder(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Close() }()
	rec.Record(coreEvent("run_1"))

	// A tight size cap with a realistic age: only the active batch exists,
	// and as the newest batch it must survive both the age pass and the
	// size pass.
	if err := PruneRecordedRuns(baseDir, 30*24*time.Hour, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, rec.SessionID())); err != nil {
		t.Fatalf("active batch must never be pruned: %v", err)
	}
}
