package recording

import (
	"os"
	"path/filepath"
	"testing"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func TestRecordedCompactionPreservesTypedSummary(t *testing.T) {
	want := protocol.CompactionEvent{ID: "compact", Status: protocol.CompactionCompleted, Trigger: protocol.CompactionAuto, Summary: "final summary", BeforeTokens: 1000, AfterTokens: 100}
	rec := recordedEventFrom(core.Event{Type: core.EventCompaction, Payload: want})
	event, err := rec.Event()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := event.Payload.(protocol.CompactionEvent)
	if !ok || got != want {
		t.Fatalf("replay lost compaction: %#v", event.Payload)
	}
}

func TestDecodeRecordedPayloadRejectsUnknownCompactionState(t *testing.T) {
	_, err := decodeRecordedPayload(core.ProtocolVersion, core.EventCompaction, []byte(`{"id":"x","status":"typo","trigger":"auto"}`))
	if err == nil {
		t.Fatal("unknown compaction status was accepted")
	}
}

func TestSafePathPart(t *testing.T) {
	got := safePathPart("../run:1")
	if got != "___run_1" {
		t.Fatalf("safePathPart = %q", got)
	}
}

func TestRecordErrorNilOrEmpty(t *testing.T) {
	var nilRecorder *EventRecorder
	if err := nilRecorder.RecordError(core.Event{RunID: "run_1"}); err != nil {
		t.Fatalf("nil recorder should not error: %v", err)
	}

	r := &EventRecorder{}
	if err := r.RecordError(core.Event{RunID: ""}); err != nil {
		t.Fatalf("empty core.RunID should not error: %v", err)
	}
}

func TestRecordErrorReportsFailures(t *testing.T) {
	base := t.TempDir()
	r := &EventRecorder{
		sessionID: "test",
		runDir:    filepath.Join(base, "test"),
	}

	// Make the run directory unwritable so MkdirAll fails.
	if err := os.MkdirAll(r.runDir, 0o700); err != nil {
		t.Fatalf("setup run dir: %v", err)
	}
	if err := os.Chmod(r.runDir, 0o500); err != nil {
		t.Fatalf("chmod run dir: %v", err)
	}
	defer os.Chmod(r.runDir, 0o700)

	if err := r.RecordError(core.Event{RunID: "run_1"}); err == nil {
		t.Fatal("expected error for unwritable run dir")
	}
	if err := r.Close(); err == nil {
		t.Fatal("Close should report the earlier write failure")
	}
}

func TestNewEventRecorderCreatesRunDirectoryLazily(t *testing.T) {
	base := t.TempDir()
	recorder, err := NewEventRecorder(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(recorder.runDir); !os.IsNotExist(err) {
		t.Fatalf("run batch should not exist before its first event: %v", err)
	}
	if err := recorder.RecordError(core.Event{RunID: "run_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(recorder.runDir, "run_1", "events.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestPruneEmptyBatches(t *testing.T) {
	base := t.TempDir()
	emptyBatch := filepath.Join(base, "0000")
	if err := os.MkdirAll(emptyBatch, 0o700); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(base, "0001", "run_1", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(retained), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(retained, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	pruneEmptyBatches(base)

	if _, err := os.Stat(emptyBatch); !os.IsNotExist(err) {
		t.Fatalf("empty batch was not removed: %v", err)
	}
	if _, err := os.Stat(retained); err != nil {
		t.Fatalf("recorded batch was removed: %v", err)
	}
}
