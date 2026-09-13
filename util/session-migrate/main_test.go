package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSessionJSON(t *testing.T, path, id string, messages string) {
	t.Helper()
	body := `{"id":` + quote(id) + `,"cwd":"/tmp/work","messages":[` + messages + `]}`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func quote(s string) string {
	return `"` + s + `"`
}

func TestMigrateAllConvertsLegacySessionAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	id := "legacy-session"
	writeSessionJSON(t, filepath.Join(root, id, "session.json"), id,
		`{"role":"user","content":"hello"}`)

	if got := migrateAll(root, true); len(got) != 1 || got[0].Status != "would-migrate" {
		t.Fatalf("dry run = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, id, transcriptFileName)); !os.IsNotExist(err) {
		t.Fatal("dry run wrote transcript")
	}
	if got := migrateAll(root, false); len(got) != 1 || got[0].Status != "migrated" || got[0].Err != nil {
		t.Fatalf("migration = %+v", got)
	}
	if got := migrateAll(root, false); len(got) != 1 || got[0].Status != "skipped" {
		t.Fatalf("second migration = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, id, "migration-backup", "session.v1.json")); err != nil {
		t.Fatalf("legacy backup: %v", err)
	}
	messages, err := loadTranscript(filepath.Join(root, id))
	if err != nil || len(messages) != 1 {
		t.Fatalf("transcript = %+v, %v", messages, err)
	}

	// The v2 snapshot must keep the original messages and carry the watermark.
	data, err := os.ReadFile(filepath.Join(root, id, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["format_version"] != float64(2) || snapshot["transcript_seq"] != float64(1) {
		t.Fatalf("v2 fields = %+v", snapshot)
	}
	msgs, ok := snapshot["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages were not preserved: %+v", snapshot["messages"])
	}
}

func TestMigrationRecoversCompatibleSessionBackup(t *testing.T) {
	root := t.TempDir()
	id := "legacy-with-backup"
	d := filepath.Join(root, id)
	writeSessionJSON(t, filepath.Join(d, "session.json"), id, `{"role":"assistant","content":"recent"}`)
	writeSessionJSON(t, filepath.Join(d, "session.json.bak"), id,
		`{"role":"user","content":"old"},{"role":"assistant","content":"recent"}`)

	if got := migrateOne(root, id, false); got.Err != nil {
		t.Fatal(got.Err)
	}
	messages, err := loadTranscript(d)
	if err != nil || len(messages) != 2 {
		t.Fatalf("recovered transcript = %d messages, %v", len(messages), err)
	}
}

// A partial migration followed by an old binary writing more v1 messages is a
// mixed state: the existing transcript no longer covers the snapshot's
// messages, and silently accepting it would let crash recovery duplicate them.
func TestMigrationRejectsShortExistingTranscript(t *testing.T) {
	root := t.TempDir()
	id := "mixed-state"
	d := filepath.Join(root, id)
	writeSessionJSON(t, filepath.Join(d, "session.json"), id,
		`{"role":"user","content":"one"},{"role":"assistant","content":"two"}`)
	if err := writeTranscript(filepath.Join(d, transcriptFileName),
		[]json.RawMessage{[]byte(`{"role":"user","content":"one"}`)}); err != nil {
		t.Fatal(err)
	}

	result := migrateOne(root, id, false)
	if result.Status != "failed" || result.Err == nil {
		t.Fatalf("migration = %+v, want failure on mixed state", result)
	}
}

func TestMigrationRejectsDivergentExistingTranscript(t *testing.T) {
	root := t.TempDir()
	id := "divergent-existing-transcript"
	d := filepath.Join(root, id)
	writeSessionJSON(t, filepath.Join(d, "session.json"), id,
		`{"role":"user","content":"current"}`)
	if err := writeTranscript(filepath.Join(d, transcriptFileName),
		[]json.RawMessage{[]byte(`{"role":"user","content":"different"}`)}); err != nil {
		t.Fatal(err)
	}

	result := migrateOne(root, id, false)
	if result.Status != "failed" || result.Err == nil {
		t.Fatalf("migration = %+v, want failure for divergent histories", result)
	}
}

// A backup whose tail diverges from the current messages must not be merged.
func TestMigrationIgnoresDivergentBackup(t *testing.T) {
	root := t.TempDir()
	id := "divergent-backup"
	d := filepath.Join(root, id)
	writeSessionJSON(t, filepath.Join(d, "session.json"), id, `{"role":"user","content":"current"}`)
	writeSessionJSON(t, filepath.Join(d, "session.json.bak"), id, `{"role":"user","content":"different"}`)

	if got := migrateOne(root, id, false); got.Err != nil {
		t.Fatal(got.Err)
	}
	messages, err := loadTranscript(d)
	if err != nil || len(messages) != 1 {
		t.Fatalf("divergent backup was merged: %d messages, %v", len(messages), err)
	}
}

// An id mismatch between the directory and the snapshot must be rejected.
func TestMigrationRejectsMismatchedID(t *testing.T) {
	root := t.TempDir()
	id := "session-a"
	writeSessionJSON(t, filepath.Join(root, id, "session.json"), "session-b", `{"role":"user","content":"x"}`)
	if got := migrateOne(root, id, false); got.Status != "failed" || got.Err == nil {
		t.Fatalf("mismatched id accepted: %+v", got)
	}
}

func TestWriteAtomicReportsDirectorySyncFailure(t *testing.T) {
	original := syncMigrationDir
	syncMigrationDir = func(string) error { return errors.New("injected directory sync failure") }
	t.Cleanup(func() { syncMigrationDir = original })

	err := writeAtomic(filepath.Join(t.TempDir(), "session.json"), []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "directory sync failure") {
		t.Fatalf("writeAtomic error = %v", err)
	}
}
