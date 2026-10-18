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
// writeBatch plants one session with a single recorded run. The batch's age is
// its runs/ directory mtime, which is what retention looks at.
func writeBatch(t *testing.T, sessionsRoot, name string, size int, mod time.Time) {
	t.Helper()
	runsDir := filepath.Join(sessionsRoot, name, runsSubdir)
	runDir := filepath.Join(runsDir, "run_1")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	if err := os.WriteFile(filepath.Join(runDir, "events.jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(runsDir, mod, mod); err != nil {
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
	if _, err := os.Stat(filepath.Join(baseDir, "old", runsSubdir)); !os.IsNotExist(err) {
		t.Fatalf("stale runs survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "fresh", runsSubdir)); err != nil {
		t.Fatalf("fresh runs were pruned: %v", err)
	}
	// The session directory itself is owned by the session store and must
	// outlive its recordings.
	for _, name := range []string{"old", "fresh"} {
		if _, err := os.Stat(filepath.Join(baseDir, name)); err != nil {
			t.Fatalf("pruning removed the session directory %s: %v", name, err)
		}
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
		if _, err := os.Stat(filepath.Join(baseDir, removed, runsSubdir)); !os.IsNotExist(err) {
			t.Fatalf("runs of %s must be pruned under the size cap", removed)
		}
	}
	if _, err := os.Stat(filepath.Join(baseDir, "b_youngest", runsSubdir)); err != nil {
		t.Fatalf("newest runs must survive: %v", err)
	}
}

func TestPruneOldRunsWithinActiveSession(t *testing.T) {
	root := t.TempDir()
	writeBatch(t, root, "session", 1024, time.Now().Add(-48*time.Hour))
	newDir := filepath.Join(root, "session", runsSubdir, "run_2")
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "events.jsonl"), make([]byte, 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PruneRecordedRuns(root, 30*24*time.Hour, 1024); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "session", runsSubdir, "run_1")); !os.IsNotExist(err) {
		t.Fatalf("old run retained: %v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatal(err)
	}
}

func TestPruneProtectsActiveRunUntilReleased(t *testing.T) {
	root := t.TempDir()
	rec, err := newTestRecorder(t, root)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	if err := rec.BeginRun("run_1", "session"); err != nil {
		t.Fatal(err)
	}
	recordRun(t, rec, "run_1")
	path := filepath.Join(rec.RunDir("run_1"), "events.jsonl")
	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := PruneRecordedRuns(root, 30*24*time.Hour, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active run removed: %v", err)
	}
	rec.EndRun("run_1")
	if err := PruneRecordedRuns(root, 30*24*time.Hour, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("released run retained: %v", err)
	}
}

func TestContextObservationIsNotRecorded(t *testing.T) {
	rec, err := newTestRecorder(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	if err := rec.RecordError(core.Event{RunID: "run_1", Type: core.EventContextUpdated}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rec.RunDir("run_1")); !os.IsNotExist(err) {
		t.Fatalf("transient context created recording: %v", err)
	}
}

func TestPruneRecordedRunsKeepsActiveBatch(t *testing.T) {
	baseDir := t.TempDir()
	// An active recorder has a just-created batch directory.
	rec, err := newTestRecorder(t, baseDir)
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

// A session that is being written to must survive the retention sweep even when
// its runs/ directory mtime is old: appending to events.jsonl updates the file,
// not the directory above it.
func TestPruneKeepsSessionWithRecentWrites(t *testing.T) {
	root := t.TempDir()
	writeBatch(t, root, "writing", 100, time.Now().Add(-40*24*time.Hour))
	writeBatch(t, root, "newer", 100, time.Now())
	// The old-looking batch has a fresh events file: it is active.
	events := filepath.Join(root, "writing", runsSubdir, "run_1", "events.jsonl")
	now := time.Now()
	if err := os.Chtimes(events, now, now); err != nil {
		t.Fatal(err)
	}

	if err := PruneRecordedRuns(root, 30*24*time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "writing", runsSubdir)); err != nil {
		t.Fatalf("active session's runs were pruned: %v", err)
	}
}

// A genuinely idle session is still collected.
func TestPruneRemovesIdleSessionRuns(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-40 * 24 * time.Hour)
	writeBatch(t, root, "idle", 100, old)
	events := filepath.Join(root, "idle", runsSubdir, "run_1", "events.jsonl")
	if err := os.Chtimes(events, old, old); err != nil {
		t.Fatal(err)
	}

	if err := PruneRecordedRuns(root, 30*24*time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "idle", runsSubdir)); !os.IsNotExist(err) {
		t.Fatal("idle session's runs were kept")
	}
	// The conversation snapshot must outlive its recordings.
	if _, err := os.Stat(filepath.Join(root, "idle")); err != nil {
		t.Fatalf("session directory was removed: %v", err)
	}
}
