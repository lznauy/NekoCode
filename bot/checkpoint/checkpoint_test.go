package checkpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestChangeKindKeepsManifestJSONCompatible(t *testing.T) {
	var item entry
	if err := json.Unmarshal([]byte(`{"path":"main.go","change":"modified"}`), &item); err != nil {
		t.Fatal(err)
	}
	if item.Change != ChangeModified {
		t.Fatalf("decoded change = %q, want %q", item.Change, ChangeModified)
	}
	data, err := json.Marshal(entry{Path: "main.go", Change: ChangeCreated})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"path":"main.go","before":"","change":"created"}` {
		t.Fatalf("encoded entry = %s", data)
	}
}

func TestFinalizeSkipsManifestWriteWhenChangeKindIsUnchanged(t *testing.T) {
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	turn, err := m.BeginMessage("session", "create")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "created.txt")
	if err := m.Capture(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("created"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := m.turnDir("session", turn)
	manifestPath := filepath.Join(dir, manifestName)
	savedPath := filepath.Join(dir, "saved-manifest")
	if err := os.Rename(manifestPath, savedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifestPath, 0o700); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Remove(manifestPath)
		_ = os.Rename(savedPath, manifestPath)
	}()
	if err := m.Finalize(path); err != nil {
		t.Fatalf("unchanged finalize rewrote manifest: %v", err)
	}
}

func TestFinalizeNoOpFailureRetainsSnapshotAndCapture(t *testing.T) {
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	turn, err := m.BeginMessage("session", "no-op")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "unchanged.txt")
	if err := os.WriteFile(path, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Capture(path); err != nil {
		t.Fatal(err)
	}
	dir := m.turnDir("session", turn)
	manifestPath := filepath.Join(dir, manifestName)
	savedPath := filepath.Join(dir, "saved-manifest")
	if err := os.Rename(manifestPath, savedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifestPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := m.Finalize(path); err == nil {
		t.Fatal("finalize unexpectedly succeeded with an unwritable manifest")
	}
	snapshotPath := filepath.Join(dir, snapshotName(path))
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("failed finalize removed snapshot: %v", err)
	}
	if len(m.active.Entries) != 1 {
		t.Fatalf("failed finalize removed capture: %+v", m.active.Entries)
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(savedPath, manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := m.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if len(m.active.Entries) != 0 {
		t.Fatalf("successful retry retained no-op capture: %+v", m.active.Entries)
	}
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("successful retry retained snapshot: %v", err)
	}
}

func TestRewindRestoresModifiedCreatedAndDeletedFiles(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	modified := filepath.Join(work, "modified.bin")
	created := filepath.Join(work, "created.txt")
	deleted := filepath.Join(work, "deleted.txt")
	original := []byte{0, 1, 2, '\r', '\n', 255}
	if err := os.WriteFile(modified, original, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deleted, []byte("restore me"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(root)
	m.Activate("session-1", nil, 0)
	turn, err := m.BeginMessage("session-1", "Update binary handling")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{modified, created, deleted} {
		if err := m.Capture(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(modified, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{modified, created, deleted} {
		if err := m.Finalize(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Finish("session-1"); err != nil {
		t.Fatal(err)
	}
	history, err := m.History("session-1")
	if err != nil || len(history) != 1 || len(history[0].Changes) != 3 {
		t.Fatalf("history = %+v err=%v", history, err)
	}

	// Simulate session reload: only the persisted turn index is carried in
	// session.json; manifests and bytes remain on disk.
	reloaded := New(root)
	reloaded.Activate("session-1", m.Index("session-1"), m.Next("session-1"))
	result, err := reloaded.Rewind("session-1", turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 3 {
		t.Fatalf("rollback changes = %+v, want 3 unique paths", result.Changes)
	}
	actions := make(map[string]string, len(result.Changes))
	for _, change := range result.Changes {
		actions[change.Path] = change.Action
	}
	if actions[created] != RollbackRemovedCreatedFile || actions[modified] != RollbackRestoredFile || actions[deleted] != RollbackRestoredDeletedFile {
		t.Fatalf("rollback actions = %+v", actions)
	}
	got, err := os.ReadFile(modified)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("modified bytes = %v, want %v", got, original)
	}
	info, err := os.Stat(modified)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("modified mode = %v, want 0640", info.Mode().Perm())
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created file survived rewind: %v", err)
	}
	if got, err := os.ReadFile(deleted); err != nil || string(got) != "restore me" {
		t.Fatalf("deleted file restore = %q err=%v", got, err)
	}
	if index := reloaded.Index("session-1"); len(index) != 0 {
		t.Fatalf("rewound index = %v, want empty", index)
	}
	pending := reloaded.Recovered("session-1")
	if len(pending) != 1 || pending[0].RewindID != result.RewindID || pending[0].Turn != turn {
		t.Fatalf("pending committed rewind = %+v", pending)
	}
	if err := reloaded.AcknowledgeRecovered("session-1", result.RewindID); err != nil {
		t.Fatal(err)
	}
	if next, err := reloaded.BeginMessage("session-1", "Continue work"); err != nil || next != "2" {
		t.Fatalf("next turn after rewind = %q err=%v, want 2", next, err)
	}
}

func TestHistoryRejectsManifestWithoutValidAuthentication(t *testing.T) {
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	turn, err := m.BeginMessage("session", "original")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.turnDir("session", turn), manifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), "original", "modified", 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	history, err := m.History("session")
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("unauthenticated manifest history = %+v err=%v", history, err)
	}
}

func TestActivateMigratesStrictlyValidLegacyManifest(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	path := filepath.Join(work, "legacy.txt")
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	turnDir := filepath.Join(root, "session", "1")
	if err := secureDir(turnDir); err != nil {
		t.Fatal(err)
	}
	legacy := manifest{
		Turn: "1",
		Entries: []entry{{
			Path: path, Before: "file", Change: ChangeModified,
			Snapshot: snapshotName(path), Mode: 0o644,
		}},
	}
	if err := writePrivateFile(filepath.Join(turnDir, snapshotName(path)), []byte("before")); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(filepath.Join(turnDir, manifestName), data); err != nil {
		t.Fatal(err)
	}

	m := New(root)
	m.Activate("session", []string{"1"}, 1)
	history, err := m.History("session")
	if err != nil || len(history) != 1 {
		t.Fatalf("legacy history = %+v err=%v", history, err)
	}
	migrated, err := m.readManifest(turnDir)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Version != 2 || migrated.MAC == "" || !validContentDigest(migrated.Entries[0].Digest) {
		t.Fatalf("legacy manifest was not authenticated: %+v", migrated)
	}
	if _, err := m.Rewind("session", "1"); err != nil {
		t.Fatal(err)
	}
	if restored, err := os.ReadFile(path); err != nil || string(restored) != "before" {
		t.Fatalf("legacy rewind = %q err=%v", restored, err)
	}
}

func TestConcurrentManagersPublishOneCompleteAuthenticationKey(t *testing.T) {
	root := t.TempDir()
	start := make(chan struct{})
	errCh := make(chan error, 8)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			m := New(root)
			sessionID := "session-" + strconv.Itoa(worker)
			m.Activate(sessionID, nil, 0)
			if _, err := m.BeginMessage(sessionID, "concurrent"); err != nil {
				errCh <- err
				return
			}
			if err := m.Finish(sessionID); err != nil {
				errCh <- err
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	key, err := readAuthKey(filepath.Join(root, macKeyName))
	if err != nil || len(key) != 32 {
		t.Fatalf("published authentication key length = %d err=%v", len(key), err)
	}
}

func TestAuthenticatedManifestCannotMoveAcrossSessions(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	m.Activate("session-a", nil, 0)
	if _, err := m.BeginMessage("session-a", "signed for A"); err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session-a"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(m.turnDir("session-a", "1"), manifestName)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	destinationDir := m.turnDir("session-b", "1")
	if err := secureDir(destinationDir); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(filepath.Join(destinationDir, manifestName), data); err != nil {
		t.Fatal(err)
	}
	m.Activate("session-b", []string{"1"}, 1)
	if _, err := m.History("session-b"); err == nil || !strings.Contains(err.Error(), "manifest session") {
		t.Fatalf("cross-session manifest error = %v", err)
	}
}

func TestFinishRetainsNoOpMessageAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "same.txt")
	if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	turn, err := m.BeginMessage("session", "Inspect this file without changing it")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Capture(path); err != nil {
		t.Fatal(err)
	}
	if err := m.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	if turns := m.Index("session"); !reflect.DeepEqual(turns, []string{turn}) {
		t.Fatalf("message anchor index = %v, want [%s]", turns, turn)
	}
	history, err := m.History("session")
	if err != nil || len(history) != 1 || history[0].UserMessage != "Inspect this file without changing it" || len(history[0].Changes) != 0 {
		t.Fatalf("message anchor history = %+v err=%v", history, err)
	}
	if _, err := m.Rewind("session", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.turnDir("session", turn)); !os.IsNotExist(err) {
		t.Fatalf("rewound message anchor survived: %v", err)
	}
}

func TestBeginRejectsOverlappingTurns(t *testing.T) {
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	if _, err := m.BeginMessage("session", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.BeginMessage("session", "overlap"); err == nil {
		t.Fatal("second Begin replaced an active checkpoint turn")
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	if turn, err := m.BeginMessage("session", "second"); err != nil || turn != "2" {
		t.Fatalf("Begin after Finish = %q, %v", turn, err)
	}
}

func TestFinishKeepsLatestHundredMessageAnchors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	m.Activate("session", nil, 0)
	for i := 1; i <= 102; i++ {
		if _, err := m.BeginMessage("session", "Change file"); err != nil {
			t.Fatal(err)
		}
		if err := m.Capture(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte{byte(i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := m.Finalize(path); err != nil {
			t.Fatal(err)
		}
		if err := m.Finish("session"); err != nil {
			t.Fatal(err)
		}
	}

	wantTurns := make([]string, 0, MaxTurnsPerSession)
	for i := 3; i <= 102; i++ {
		wantTurns = append(wantTurns, strconv.Itoa(i))
	}
	if got := m.Index("session"); !reflect.DeepEqual(got, wantTurns) {
		t.Fatalf("retained turns = %v, want %v", got, wantTurns)
	}
	for _, turn := range []string{"1", "2"} {
		if _, err := os.Stat(m.turnDir("session", turn)); !os.IsNotExist(err) {
			t.Fatalf("pruned turn %s survived: %v", turn, err)
		}
	}
	history, err := m.History("session")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != MaxTurnsPerSession || history[0].Turn != "102" || history[len(history)-1].Turn != "3" {
		t.Fatalf("history order = %+v", history)
	}
}

func TestRewindRejectsInFlightMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	if _, err := m.BeginMessage("session", "Modify busy file"); err != nil {
		t.Fatal(err)
	}
	if err := m.Capture(path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rewind("session", ""); err == nil {
		t.Fatal("rewind succeeded while mutation was in flight")
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rewind("session", ""); err != nil {
		t.Fatal(err)
	}
}

func TestBeginStoresBoundedSingleLineMessagePreview(t *testing.T) {
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	message := "  first \x1bline\n\t" + strings.Repeat("界", maxMessageRunes) + "  "
	if _, err := m.BeginMessage("session", message); err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	history, err := m.History("session")
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %+v err=%v", history, err)
	}
	if strings.ContainsAny(history[0].UserMessage, "\x1b\n\t") || !strings.HasSuffix(history[0].UserMessage, "…") || len([]rune(history[0].UserMessage)) != maxMessageRunes {
		t.Fatalf("message preview = %q", history[0].UserMessage)
	}
}

func TestHistoryAndRewindStripPersistedFormatControls(t *testing.T) {
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	turn, err := m.BeginMessage("session", "safe")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	mf, err := m.readManifest(m.turnDir("session", turn))
	if err != nil {
		t.Fatal(err)
	}
	mf.UserMessage = "visible\u202Espoof\u2066text"
	if err := m.writeManifest(m.turnDir("session", turn), mf); err != nil {
		t.Fatal(err)
	}
	history, err := m.History("session")
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %+v err=%v", history, err)
	}
	if strings.ContainsAny(history[0].UserMessage, "\u202E\u2066") {
		t.Fatalf("history exposed bidi controls: %q", history[0].UserMessage)
	}
	result, err := m.Rewind("session", turn)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(result.UserMessage, "\u202E\u2066") {
		t.Fatalf("rewind result exposed bidi controls: %q", result.UserMessage)
	}
}

func TestRewindSnapshotDigestFailureLeavesEveryFileUnchanged(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	first := filepath.Join(work, "a.txt")
	second := filepath.Join(work, "b.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(root)
	m.Activate("session", nil, 0)
	turn, err := m.BeginMessage("session", "change both")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		if err := m.Capture(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := m.Finalize(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.turnDir("session", turn), snapshotName(second)), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rewind("session", turn); err == nil {
		t.Fatal("rewind unexpectedly succeeded with a tampered snapshot")
	}
	for _, path := range []string{first, second} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "after" {
			t.Fatalf("%s changed after failed rewind: %q err=%v", path, data, err)
		}
	}
	if got := m.Index("session"); !reflect.DeepEqual(got, []string{turn}) {
		t.Fatalf("failed rewind changed checkpoint index: %v", got)
	}
}

func TestActivateRecoversInterruptedRewindJournal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	m.Activate("session", nil, 0)
	turn, err := m.BeginMessage("session", "change")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Capture(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}

	stageDir, err := os.MkdirTemp(filepath.Join(root, "session"), ".rewind-")
	if err != nil {
		t.Fatal(err)
	}
	rollbackDir := filepath.Join(stageDir, rewindRollbackDir)
	if err := secureDir(rollbackDir); err != nil {
		t.Fatal(err)
	}
	rollbackName := snapshotName(path)
	if err := writePrivateFile(filepath.Join(rollbackDir, rollbackName), []byte("after")); err != nil {
		t.Fatal(err)
	}
	journal := rewindJournal{
		ID: filepath.Base(stageDir), Session: "session", Phase: "staged", Target: turn, Turns: []string{turn},
		Files:   []rewindRollback{{Path: path, Exists: true, Snapshot: rollbackName, Digest: contentDigest([]byte("after")), Mode: 0o600}},
		Changes: []RollbackChange{{Path: path, Action: RollbackRestoredFile}},
	}
	if err := m.writeRewindJournal(stageDir, journal); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(m.turnDir("session", turn), filepath.Join(stageDir, turn)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}

	reloaded := New(root)
	reloaded.Activate("session", []string{turn}, 1)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "after" {
		t.Fatalf("recovered workspace = %q err=%v", data, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("recovered mode = %v err=%v", info, err)
	}
	if got := reloaded.Index("session"); !reflect.DeepEqual(got, []string{turn}) {
		t.Fatalf("recovered checkpoint index = %v", got)
	}
	if _, err := os.Stat(reloaded.turnDir("session", turn)); err != nil {
		t.Fatalf("checkpoint turn was not restored: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(root, "session", ".rewind-*")); len(matches) != 0 {
		t.Fatalf("rewind staging area survived recovery: %v", matches)
	}
}

func TestRewindRejectsTamperedManifestPathsAndSnapshots(t *testing.T) {
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	turn, err := m.BeginMessage("session", "safe")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	mf, err := m.readManifest(m.turnDir("session", turn))
	if err != nil {
		t.Fatal(err)
	}
	mf.Entries = []entry{{Path: filepath.Join(t.TempDir(), "victim"), Before: "file", Change: ChangeModified, Snapshot: "../outside", Mode: 0o644}}
	if err := m.writeManifest(m.turnDir("session", turn), mf); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rewind("session", turn); err == nil || !strings.Contains(err.Error(), "invalid snapshot") {
		t.Fatalf("tampered manifest error = %v", err)
	}
}

func TestRotateMessageCreatesAFileRestoreBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	m.Activate("session", nil, 0)
	first, err := m.BeginMessage("session", "First request")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Capture(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if err := m.RotateMessage("Steer the running agent"); err != nil {
		t.Fatal(err)
	}
	if err := m.Capture(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("steered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	history, err := m.History("session")
	if err != nil || len(history) != 2 || history[0].UserMessage != "Steer the running agent" || history[1].Turn != first {
		t.Fatalf("history = %+v err=%v", history, err)
	}
	if _, err := m.Rewind("session", history[0].Turn); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "first" {
		t.Fatalf("rewind to steering boundary = %q err=%v", data, err)
	}
}

func TestHistoryReconcilesMissingPersistedManifest(t *testing.T) {
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	first, err := m.BeginMessage("session", "First")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	second, err := m.BeginMessage("session", "Second")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(m.turnDir("session", first)); err != nil {
		t.Fatal(err)
	}
	history, err := m.History("session")
	if err != nil || len(history) != 1 || history[0].Turn != second {
		t.Fatalf("reconciled history = %+v err=%v", history, err)
	}
	if got := m.Index("session"); !reflect.DeepEqual(got, []string{second}) {
		t.Fatalf("reconciled index = %v", got)
	}
}

func TestRotateMessageFailureKeepsPreviousAnchorActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(t.TempDir())
	m.Activate("session", nil, 0)
	first, err := m.BeginMessage("session", "Initial request")
	if err != nil {
		t.Fatal(err)
	}
	// Block creation of the next checkpoint directory without disturbing the
	// current manifest, simulating a filesystem failure during rotation.
	if err := os.WriteFile(m.turnDir("session", "2"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.RotateMessage("Steering request"); err == nil {
		t.Fatal("rotation unexpectedly succeeded")
	}
	if err := m.Capture(path); err != nil {
		t.Fatalf("previous anchor was not restored: %v", err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if err := m.Finish("session"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rewind("session", first); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "before" {
		t.Fatalf("restored file = %q err=%v", data, err)
	}
}
