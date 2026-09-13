// Command session-migrate is a one-off utility that converts legacy v1
// sessions (session.json with the full history embedded in "messages") to the
// split v2 format (session.json as the compaction-friendly snapshot plus
// transcript.jsonl as the append-only full history).
//
// It is deliberately standalone: it depends only on the standard library and
// treats session.json as an opaque object, so it keeps working regardless of
// how the main project's snapshot schema evolves. Run it with:
//
//	go run ./util/session-migrate            # migrate everything
//	go run ./util/session-migrate --dry-run  # report only
//	go run ./util/session-migrate --verify   # validate v2 sessions
//	go run ./util/session-migrate --session <id>
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const transcriptFileName = "transcript.jsonl"

type MigrationResult struct {
	ID     string
	Status string
	Err    error
}

func main() {
	dryRun := flag.Bool("dry-run", false, "report changes without writing files")
	verify := flag.Bool("verify", false, "validate sessions without writing files")
	id := flag.String("session", "", "migrate only one session id")
	root := flag.String("root", defaultSessionsDir(), "sessions directory")
	flag.Parse()
	failed := false
	var results []MigrationResult
	if *id != "" {
		results = []MigrationResult{migrateOne(*root, *id, *dryRun || *verify)}
	} else {
		results = migrateAll(*root, *dryRun || *verify)
	}
	for _, result := range results {
		if result.Err != nil {
			failed = true
			fmt.Fprintf(os.Stderr, "%s\tfailed\t%v\n", result.ID, result.Err)
			continue
		}
		fmt.Printf("%s\t%s\n", result.ID, result.Status)
	}
	if failed {
		os.Exit(1)
	}
}

func defaultSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "sessions"
	}
	return filepath.Join(home, ".nekocode", "sessions")
}

func migrateAll(root string, dryRun bool) []MigrationResult {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []MigrationResult{{Status: "failed", Err: err}}
	}
	var results []MigrationResult
	for _, entry := range entries {
		if !entry.IsDir() || validateID(entry.Name()) != nil {
			continue
		}
		results = append(results, migrateOne(root, entry.Name(), dryRun))
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}

// migrateOne performs the v1 → v2 conversion for a single session directory.
// It is idempotent: an already-migrated session reports "skipped".
func migrateOne(root, id string, dryRun bool) MigrationResult {
	result := MigrationResult{ID: id}
	d := filepath.Join(root, id)
	sessionPath := filepath.Join(d, "session.json")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		result.Status, result.Err = "failed", err
		return result
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(data, &snapshot); err != nil {
		result.Status, result.Err = "failed", err
		return result
	}
	if err := checkSessionID(snapshot, id); err != nil {
		result.Status, result.Err = "failed", err
		return result
	}

	if version, ok := intField(snapshot, "format_version"); ok && version >= 2 {
		messages, err := loadTranscript(d)
		if err != nil {
			result.Status, result.Err = "failed", err
		} else if seq, ok := intField(snapshot, "transcript_seq"); !ok || seq != len(messages) {
			result.Status, result.Err = "failed", fmt.Errorf("transcript count %d, want %d", len(messages), seq)
		} else {
			result.Status = "skipped"
		}
		return result
	}
	if dryRun {
		result.Status = "would-migrate"
		return result
	}

	messages, err := messageField(snapshot, "messages")
	if err != nil {
		result.Status, result.Err = "failed", err
		return result
	}
	transcriptMessages := recoverBackupMessages(d, messages)
	transcriptPath := filepath.Join(d, transcriptFileName)
	transcriptExists := false
	if _, err := os.Lstat(transcriptPath); err == nil {
		transcriptExists = true
		existing, loadErr := loadTranscript(d)
		if loadErr != nil {
			result.Status, result.Err = "failed", fmt.Errorf("existing transcript is invalid; refusing to overwrite: %w", loadErr)
			return result
		}
		// A partial migration followed by an old binary writing more v1
		// messages can leave an existing transcript that no longer covers the
		// snapshot's messages. Accepting it would break the suffix invariant
		// and make crash recovery duplicate the uncovered messages.
		if len(existing) < len(messages) {
			result.Status, result.Err = "failed", fmt.Errorf(
				"existing transcript has %d records but snapshot carries %d messages; refusing to migrate a mixed v1/v2 state", len(existing), len(messages))
			return result
		}
		if !jsonSuffix(existing, messages) {
			result.Status, result.Err = "failed", fmt.Errorf("existing transcript does not end with the snapshot messages; refusing to migrate divergent histories")
			return result
		}
		transcriptMessages = existing
	} else if !os.IsNotExist(err) {
		result.Status, result.Err = "failed", err
		return result
	}

	// Preserve the pre-migration v1 file verbatim before any modification.
	backupPath := filepath.Join(d, "migration-backup", "session.v1.json")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		if err := writeAtomic(backupPath, data); err != nil {
			result.Status, result.Err = "failed", err
			return result
		}
	} else if err != nil {
		result.Status, result.Err = "failed", err
		return result
	} else if backup, readErr := os.ReadFile(backupPath); readErr != nil || !bytes.Equal(backup, data) {
		if readErr == nil {
			readErr = fmt.Errorf("existing migration backup differs from session.json")
		}
		result.Status, result.Err = "failed", readErr
		return result
	}

	if !transcriptExists {
		if err := writeTranscript(transcriptPath, transcriptMessages); err != nil {
			result.Status, result.Err = "failed", err
			return result
		}
	}

	// Rewrite the snapshot as v2: bump the format version and record the
	// watermark. Every other field is copied through untouched.
	snapshot["format_version"], _ = json.Marshal(2)
	snapshot["transcript_seq"], _ = json.Marshal(len(transcriptMessages))
	updated, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		result.Status, result.Err = "failed", err
		return result
	}
	if err := writeAtomic(sessionPath, append(updated, '\n')); err != nil {
		result.Status, result.Err = "failed", err
		return result
	}
	if messages, err := loadTranscript(d); err != nil || len(messages) != len(transcriptMessages) {
		if err == nil {
			err = fmt.Errorf("transcript count %d, want %d", len(messages), len(transcriptMessages))
		}
		result.Status, result.Err = "failed", err
		return result
	}
	result.Status = "migrated"
	return result
}

// recoverBackupMessages restores the longer history held in session.json.bak
// when the current messages are exactly its retained suffix. Only that
// proven relationship is accepted: merging divergent runs would invent
// history that never happened.
func recoverBackupMessages(sessionDir string, current []json.RawMessage) []json.RawMessage {
	data, err := os.ReadFile(filepath.Join(sessionDir, "session.json.bak"))
	if err != nil {
		return current
	}
	var backup map[string]json.RawMessage
	if json.Unmarshal(data, &backup) != nil {
		return current
	}
	backupMessages, err := messageField(backup, "messages")
	if err != nil || len(backupMessages) <= len(current) {
		return current
	}
	tail := backupMessages[len(backupMessages)-len(current):]
	for i := range current {
		if !jsonEqual(current[i], tail[i]) {
			return current
		}
	}
	return backupMessages
}

// -- session.json field helpers -----------------------------------------

func checkSessionID(snapshot map[string]json.RawMessage, id string) error {
	raw, ok := snapshot["id"]
	if !ok {
		return fmt.Errorf("session id mismatch: requested %q, file contains none", id)
	}
	var found string
	if err := json.Unmarshal(raw, &found); err != nil || found != id {
		return fmt.Errorf("session id mismatch: requested %q, file contains %q", id, found)
	}
	return nil
}

func intField(snapshot map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := snapshot[key]
	if !ok {
		return 0, false
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
}

func messageField(snapshot map[string]json.RawMessage, key string) ([]json.RawMessage, error) {
	raw, ok := snapshot[key]
	if !ok {
		return nil, fmt.Errorf("session.json has no %q field", key)
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, fmt.Errorf("decode %s: %w", key, err)
	}
	return messages, nil
}

// jsonEqual compares two JSON documents by value, independent of key order or
// whitespace in the source files.
func jsonEqual(a, b []byte) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return bytes.Equal(a, b)
	}
	na, _ := json.Marshal(va)
	nb, _ := json.Marshal(vb)
	return bytes.Equal(na, nb)
}

func jsonSuffix(history, suffix []json.RawMessage) bool {
	if len(suffix) > len(history) {
		return false
	}
	start := len(history) - len(suffix)
	for i := range suffix {
		if !jsonEqual(history[start+i], suffix[i]) {
			return false
		}
	}
	return true
}

// -- transcript.jsonl -----------------------------------------------------

// loadTranscript reads the append-only history and enforces contiguous
// sequence numbers. The message bodies stay opaque.
func loadTranscript(sessionDir string) ([]json.RawMessage, error) {
	data, err := os.ReadFile(filepath.Join(sessionDir, transcriptFileName))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("transcript file does not exist")
	}
	if err != nil {
		return nil, err
	}
	var messages []json.RawMessage
	for i, line := range splitLines(data) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record struct {
			Seq     int             `json:"seq"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("decode transcript record %d: %w", i+1, err)
		}
		if record.Seq != len(messages)+1 {
			return nil, fmt.Errorf("transcript sequence %d, want %d", record.Seq, len(messages)+1)
		}
		messages = append(messages, record.Message)
	}
	return messages, nil
}

// writeTranscript materializes the full history atomically.
func writeTranscript(path string, messages []json.RawMessage) error {
	var buf bytes.Buffer
	for i, message := range messages {
		record, err := json.Marshal(struct {
			Seq     int             `json:"seq"`
			Message json.RawMessage `json:"message"`
		}{Seq: i + 1, Message: message})
		if err != nil {
			return err
		}
		buf.Write(record)
		buf.WriteByte('\n')
	}
	return writeAtomic(path, buf.Bytes())
}

func splitLines(data []byte) []json.RawMessage {
	var lines []json.RawMessage
	for len(data) > 0 {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			lines = append(lines, data)
			break
		}
		lines = append(lines, data[:idx])
		data = data[idx+1:]
	}
	return lines
}

// -- shared low-level helpers ---------------------------------------------

func validateID(id string) error {
	if strings.TrimSpace(id) == "" || id == "." || id == ".." ||
		filepath.Base(id) != id || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid session id: %q", id)
	}
	return nil
}

// writeAtomic writes data to path via a temp file and rename, so a crash
// mid-write can never leave a half-written file behind.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".migrate-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncMigrationDir(filepath.Dir(path))
}

var syncMigrationDir = func(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	err = dir.Sync()
	if closeErr := dir.Close(); err == nil {
		err = closeErr
	}
	return err
}
