package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
)

func TestSnapshotSeparatesTranscriptFromActiveContext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "recent"}}
	snapshot.Transcript = []types.Message{
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "recent"},
	}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := manager.Load(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "recent" {
		t.Fatalf("active context = %+v", loaded.Messages)
	}
	if len(loaded.Transcript) != 3 || loaded.Transcript[0].Content != "old" {
		t.Fatalf("transcript = %+v", loaded.Transcript)
	}
}

// v1 sessions are no longer loadable: the reader only understands the split
// v2 format, and the error must point at the migration tool.
func TestLoadRejectsLegacyFormat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "v1 history"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	// Rewrite the file as self-contained v1: inline messages and strip v2 fields.
	path := filepath.Join(dir(), snapshot.ID, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var header snapshotHeader
	if err := json.Unmarshal(data, &header); err != nil {
		t.Fatal(err)
	}
	messages, err := readMessages(filepath.Dir(path), header)
	if err != nil {
		t.Fatal(err)
	}
	messageData, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["messages"] = messageData
	delete(fields, "format_version")
	delete(fields, "transcript_seq")
	data, err = json.MarshalIndent(fields, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = load(snapshot.ID)
	if err == nil || !strings.Contains(err.Error(), "session-migrate") {
		t.Fatalf("v1 session should be rejected with a migration hint: %v", err)
	}
}

func TestLoadV2AndMigrateOnNextSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "v2 context"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	backup, err := manager.BackupCurrent("v2-fixture")
	if err != nil {
		t.Fatal(err)
	}
	v2Data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir(), snapshot.ID, "session.json")
	if err := os.WriteFile(path, v2Data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manager.Load(snapshot.ID)
	if err != nil || len(loaded.Messages) != 1 || loaded.Messages[0].Content != "v2 context" {
		t.Fatalf("v2 load = %+v err=%v", loaded, err)
	}
	if err := manager.Save(loaded); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var header snapshotHeader
	if err := json.Unmarshal(data, &header); err != nil {
		t.Fatal(err)
	}
	if header.FormatVersion != currentSnapshotVersion {
		t.Fatalf("next save did not migrate v2: version %d", header.FormatVersion)
	}
	reloaded, err := manager.Load(snapshot.ID)
	if err != nil || len(reloaded.Messages) != 1 || reloaded.Messages[0].Content != "v2 context" {
		t.Fatalf("migrated load = %+v err=%v", reloaded, err)
	}
}

// The transcript is the source of truth in v2: a session that claims persisted
// messages but lost its transcript.jsonl is corrupt, not silently rebuildable
// from the post-compaction Messages.
func TestLoadRejectsMissingTranscriptWithWatermark(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "recent"}}
	snapshot.Transcript = []types.Message{
		{Role: "user", Content: "old"},
		{Role: "user", Content: "recent"},
	}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir(), snapshot.ID, transcriptFileName)); err != nil {
		t.Fatal(err)
	}
	_, err := load(snapshot.ID)
	if err == nil || !strings.Contains(err.Error(), "transcript.jsonl is missing") {
		t.Fatalf("missing transcript with watermark should fail: %v", err)
	}
}

func TestBackupCurrentPreservesPreCompactionSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "before"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	backup, err := manager.BackupCurrent("compact-1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || !strings.Contains(backup, "pre-compact-") || !strings.Contains(backup, "-compact-1.session.json") {
		t.Fatalf("invalid backup %q", backup)
	}
	var backupSnapshot struct {
		FormatVersion int             `json:"format_version"`
		Messages      []types.Message `json:"messages"`
	}
	if err := json.Unmarshal(data, &backupSnapshot); err != nil {
		t.Fatal(err)
	}
	if backupSnapshot.FormatVersion != 2 || len(backupSnapshot.Messages) != 1 || backupSnapshot.Messages[0].Content != "before" {
		t.Fatalf("backup is not self-contained: %+v", backupSnapshot)
	}
	second, err := manager.BackupCurrent("compact-1")
	if err != nil || second == backup {
		t.Fatalf("second backup = %q, %v", second, err)
	}
}

func TestSnapshotSaveSkipsUnchangedAndReusesMessagesForMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "context"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(dir(), snapshot.ID)
	manifestPath := filepath.Join(d, "session.json")
	manifestInfo, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	messagesInfo, err := os.Stat(messagesPath(d, 0))
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := snapshot.UpdatedAt
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	afterManifest, _ := os.Stat(manifestPath)
	afterMessages, _ := os.Stat(messagesPath(d, 0))
	if !os.SameFile(manifestInfo, afterManifest) || !os.SameFile(messagesInfo, afterMessages) || snapshot.UpdatedAt != updatedAt {
		t.Fatal("unchanged save replaced durable files or changed updated_at")
	}

	snapshot.PromptTokens = 123
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	afterManifest, _ = os.Stat(manifestPath)
	afterMessages, _ = os.Stat(messagesPath(d, 0))
	if os.SameFile(manifestInfo, afterManifest) {
		t.Fatal("metadata-only save did not update session.json")
	}
	if !os.SameFile(messagesInfo, afterMessages) {
		t.Fatal("metadata-only save rewrote active messages")
	}
	if _, err := os.Stat(messagesPath(d, 1)); !os.IsNotExist(err) {
		t.Fatalf("metadata-only save created another messages slot: %v", err)
	}
	loaded, err := manager.Load(snapshot.ID)
	if err != nil || loaded.PromptTokens != 123 || len(loaded.Messages) != 1 || loaded.Messages[0].Content != "context" {
		t.Fatalf("metadata-only reload = %+v err=%v", loaded, err)
	}
}

func TestSnapshotMessageSlotsIgnoreUncommittedAndRejectCorruption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "old"}}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Messages = []types.Message{{Role: "user", Content: "committed"}}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(dir(), snapshot.ID)
	orphan, err := json.Marshal([]types.Message{{Role: "user", Content: "uncommitted"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(messagesPath(d, 0), orphan, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manager.Load(snapshot.ID)
	if err != nil || len(loaded.Messages) != 1 || loaded.Messages[0].Content != "committed" {
		t.Fatalf("uncommitted slot replaced active context: %+v err=%v", loaded, err)
	}
	if err := os.WriteFile(messagesPath(d, 1), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Load(snapshot.ID); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("corrupt active slot should fail despite complete inactive slot: %v", err)
	}
}

func TestLoadTranscriptToleratesTornTail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "one"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	dir := transcriptDirFor(t, snapshot.ID)

	// Simulate a crash between append and fsync: a half-written record that
	// the session.json watermark never confirmed.
	if err := os.WriteFile(filepath.Join(dir, transcriptFileName),
		append(mustReadTranscript(t, dir), []byte(`{"seq":2,"mess`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	messages, err := loadTranscript(dir, snapshot.TranscriptSeq)
	if err != nil {
		t.Fatalf("loadTranscript with torn tail: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "one" {
		t.Fatalf("messages = %+v", messages)
	}
	if got := mustReadTranscript(t, dir); strings.Count(string(got), "\n") != 1 || strings.Contains(string(got), `"seq":2`) {
		t.Fatalf("torn tail was not truncated: %q", got)
	}

	// A later append must land on its own line instead of concatenating.
	snapshot.Transcript = append(snapshot.Transcript, types.Message{Role: "assistant", Content: "two"})
	snapshot.Messages = snapshot.Transcript[len(snapshot.Transcript)-1:]
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	messages, err = loadTranscript(dir, snapshot.TranscriptSeq)
	if err != nil {
		t.Fatalf("reload after repair: %v", err)
	}
	if len(messages) != 2 || messages[1].Content != "two" {
		t.Fatalf("reloaded messages = %+v", messages)
	}
}

func TestLoadTranscriptRejectsMidFileCorruption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	lines := []string{
		`{"seq":1,"message":{"role":"user","content":"a"}}`,
		`{"seq":1,"message":{"role":"user","content":"duplicate-seq"}}`,
		`{"seq":2,"message":{"role":"user","content":"b"}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, transcriptFileName), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTranscript(dir, len(lines)); err == nil {
		t.Fatal("loadTranscript accepted a mid-file duplicate sequence")
	}
}

func TestLoadDoesNotTruncateCorruptConfirmedTail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "one"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	d := transcriptDirFor(t, snapshot.ID)
	path := filepath.Join(d, transcriptFileName)
	corrupt := []byte(`{"seq":1,"mess`)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := load(snapshot.ID); err == nil {
		t.Fatal("load accepted a corrupt watermark-confirmed record")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(corrupt) {
		t.Fatalf("confirmed corruption was modified: %q, %v", got, err)
	}
}

func TestLoadRejectsNegativeWatermarkBeforeTouchingTranscript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "one"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	d := transcriptDirFor(t, snapshot.ID)
	snapshotPath := filepath.Join(d, "session.json")
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"transcript_seq": 1`, `"transcript_seq": -1`, 1))
	if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(d, transcriptFileName)
	corrupt := []byte(`{"seq":1,"mess`)
	if err := os.WriteFile(transcriptPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := load(snapshot.ID); err == nil || !strings.Contains(err.Error(), "negative transcript watermark") {
		t.Fatalf("negative watermark error = %v", err)
	}
	if got, err := os.ReadFile(transcriptPath); err != nil || string(got) != string(corrupt) {
		t.Fatalf("negative watermark modified transcript: %q, %v", got, err)
	}
}

func TestLoadRecoversTranscriptTailFromZeroWatermark(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Transcript = []types.Message{{Role: "user", Content: "first message"}}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	d := transcriptDirFor(t, snapshot.ID)
	snapshotPath := filepath.Join(d, "session.json")
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"transcript_seq": 1`, `"transcript_seq": 0`, 1))
	if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := load(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "first message" {
		t.Fatalf("zero-watermark recovery lost active messages: %+v", loaded.Messages)
	}
}

func TestTranscriptSyncErrorDoesNotDuplicateOnRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New("/tmp/work")
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "one"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)

	original := syncTranscriptDir
	syncTranscriptDir = func(string) error { return errors.New("injected directory sync failure") }
	if err := manager.Save(snapshot); err == nil {
		t.Fatal("Save succeeded despite injected sync failure")
	}
	syncTranscriptDir = original
	t.Cleanup(func() { syncTranscriptDir = original })

	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	data := mustReadTranscript(t, transcriptDirFor(t, snapshot.ID))
	if got := strings.Count(string(data), "\n"); got != 1 {
		t.Fatalf("retry appended duplicate transcript records: %d lines\n%s", got, data)
	}
}

// A crash can leave a fully written final record whose newline never reached
// the disk. The repair must append the newline (not truncate the record away,
// which would drop a watermark-confirmed message) so the next append lands on
// its own line.
func TestLoadTranscriptRepairsMissingFinalNewline(t *testing.T) {
	dir := t.TempDir()
	line := `{"seq":1,"message":{"role":"user","content":"one"}}`
	if err := os.WriteFile(filepath.Join(dir, transcriptFileName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	messages, err := loadTranscript(dir, 1)
	if err != nil {
		t.Fatalf("loadTranscript without final newline: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "one" {
		t.Fatalf("messages = %+v", messages)
	}
	data, err := os.ReadFile(filepath.Join(dir, transcriptFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != line+"\n" {
		t.Fatalf("newline not repaired: %q", data)
	}

	// The next append must stay on its own line.
	if _, err := appendTranscript(dir, 1, []types.Message{
		{Role: "user", Content: "one"}, {Role: "assistant", Content: "two"},
	}); err != nil {
		t.Fatal(err)
	}
	messages, err = loadTranscript(dir, 2)
	if err != nil {
		t.Fatalf("reload after repair: %v", err)
	}
	if len(messages) != 2 || messages[1].Content != "two" {
		t.Fatalf("reloaded messages = %+v", messages)
	}
}

func mustReadTranscript(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, transcriptFileName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func transcriptDirFor(t *testing.T, id string) string {
	t.Helper()
	d := filepath.Join(dir(), id)
	if _, err := os.Stat(d); err != nil {
		t.Fatal(err)
	}
	return d
}
