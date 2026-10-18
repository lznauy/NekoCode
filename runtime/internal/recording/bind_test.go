package recording

import (
	"os"
	"path/filepath"
	"testing"

	"nekocode/runtime/internal/core"
)

func newTestRecorder(t *testing.T, base string) (*EventRecorder, error) {
	t.Helper()
	rec, err := NewEventRecorder(base)
	if err == nil {
		t.Cleanup(func() { _ = rec.Close() })
	}
	return rec, err
}

// recordRun writes one event for a run so the batch has something to move.
func recordRun(t *testing.T, rec *EventRecorder, runID core.RunID) {
	t.Helper()
	rec.Record(core.Event{ID: "evt_1", RunID: runID, Type: core.EventRunStarted})
}

func dirsIn(t *testing.T, base string) []string {
	t.Helper()
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// Binding must move what was already recorded into the real session directory,
// so a resumed turn does not leave an orphan batch behind.
func TestBindSessionMovesRecordedRuns(t *testing.T) {
	base := t.TempDir()
	rec, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	provisional := rec.SessionID()
	recordRun(t, rec, "run_1")

	if err := rec.BindSession("20261018T100000-111111111"); err != nil {
		t.Fatal(err)
	}
	if rec.SessionID() != "20261018T100000-111111111" {
		t.Fatalf("session id = %q", rec.SessionID())
	}

	// The recorded run moved with the recorder, under the session's runs/.
	if _, err := os.Stat(filepath.Join(base, "20261018T100000-111111111", runsSubdir, "run_1", "events.jsonl")); err != nil {
		t.Fatalf("recorded run did not move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, provisional)); !os.IsNotExist(err) {
		t.Fatalf("provisional directory still present: %s", provisional)
	}
	if got := dirsIn(t, base); len(got) != 1 || got[0] != "20261018T100000-111111111" {
		t.Fatalf("batches = %v, want only the bound session", got)
	}
}

// A second process resuming the same session must land in the same directory
// rather than creating another one.
func TestBindSessionMergesIntoExistingSession(t *testing.T) {
	base := t.TempDir()
	const sessionID = "20261018T100000-111111111"

	first, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	recordRun(t, first, "run_1")
	if err := first.BindSession(sessionID); err != nil {
		t.Fatal(err)
	}

	second, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	recordRun(t, second, "run_2")
	if err := second.BindSession(sessionID); err != nil {
		t.Fatal(err)
	}

	got := dirsIn(t, base)
	if len(got) != 1 || got[0] != sessionID {
		t.Fatalf("batches = %v, want a single session directory", got)
	}
	for _, run := range []string{"run_1", "run_2"} {
		if _, err := os.Stat(filepath.Join(base, sessionID, runsSubdir, run, "events.jsonl")); err != nil {
			t.Fatalf("%s missing after merge: %v", run, err)
		}
	}
}

// Binding is idempotent: every place that learns the id may call it.
func TestBindSessionIsIdempotent(t *testing.T) {
	base := t.TempDir()
	rec, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := rec.BindSession("20261018T100000-111111111"); err != nil {
			t.Fatal(err)
		}
	}
	if got := dirsIn(t, base); len(got) != 1 {
		t.Fatalf("batches = %v", got)
	}
}

func TestBindSessionSwitchKeepsEarlierRuns(t *testing.T) {
	base := t.TempDir()
	rec, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.BindSession("session_a"); err != nil {
		t.Fatal(err)
	}
	recordRun(t, rec, "run_1")
	if err := rec.BindSession("session_b"); err != nil {
		t.Fatal(err)
	}
	recordRun(t, rec, "run_2")
	for session, run := range map[string]string{"session_a": "run_1", "session_b": "run_2"} {
		if _, err := os.Stat(filepath.Join(base, session, runsSubdir, run, "events.jsonl")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRegisteredRunDoesNotFollowDefaultSession(t *testing.T) {
	root := t.TempDir()
	rec, err := newTestRecorder(t, root)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	if err := rec.BeginRun("run_1", "session_a"); err != nil {
		t.Fatal(err)
	}
	recordRun(t, rec, "run_1")
	if err := rec.BindSession("session_b"); err != nil {
		t.Fatal(err)
	}
	recordRun(t, rec, "run_1")
	events, err := loadRecordedEventFile(filepath.Join(root, "session_a", runsSubdir, "run_1", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events split after switch: %d", len(events))
	}
}

// A traversal attempt must not be able to write outside the base directory.
func TestBindSessionRejectsUnsafeID(t *testing.T) {
	base := t.TempDir()
	rec, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	provisional := rec.SessionID()
	for _, id := range []string{"", ".", "..", "../escape", "a/b", `a\b`} {
		if err := rec.BindSession(id); err == nil {
			t.Fatalf("unsafe id %q was accepted", id)
		}
	}
	if rec.SessionID() != provisional {
		t.Fatalf("session id changed to %q after a rejected bind", rec.SessionID())
	}
}

// After binding, new events must go to the bound directory.
func TestRecordAfterBindUsesSessionDir(t *testing.T) {
	base := t.TempDir()
	rec, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	recordRun(t, rec, "run_1")
	if err := rec.BindSession("20261018T100000-111111111"); err != nil {
		t.Fatal(err)
	}
	recordRun(t, rec, "run_2")

	path := filepath.Join(base, "20261018T100000-111111111", runsSubdir, "run_2", "events.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("event after bind did not use the session dir: %v", err)
	}
}

// Legacy duplicate identities cannot be repaired by renaming a directory:
// preserve both original files and report the collision explicitly.
func TestBindSessionRejectsIdentityClashWithoutMixingEvents(t *testing.T) {
	base := t.TempDir()
	const sessionID = "20261018T100000-111111111"

	first, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	recordRun(t, first, "run_1")
	if err := first.BindSession(sessionID); err != nil {
		t.Fatal(err)
	}

	second, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	recordRun(t, second, "run_1")
	if err := second.BindSession(sessionID); err == nil {
		t.Fatal("duplicate run identity accepted")
	}
	if err := second.RecordError(core.Event{RunID: "run_1", Type: core.EventRunDone}); err != nil {
		t.Fatal(err)
	}
	for rec, want := range map[*EventRecorder]int{first: 1, second: 2} {
		events, err := loadRecordedEventFile(filepath.Join(rec.RunDir("run_1"), "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != want {
			t.Fatalf("events = %d, want %d", len(events), want)
		}
	}
}

// A batch whose runs all moved leaves no temporary session directory behind.
func TestBindSessionRemovesProvisionalDir(t *testing.T) {
	base := t.TempDir()
	rec, err := newTestRecorder(t, base)
	if err != nil {
		t.Fatal(err)
	}
	provisional := rec.SessionID()
	recordRun(t, rec, "run_1")
	if err := rec.BindSession("20261018T100000-111111111"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, provisional)); !os.IsNotExist(err) {
		t.Fatalf("provisional directory %s still present", provisional)
	}
}
