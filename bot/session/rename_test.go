package session

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
)

func TestRenamePersistsWithoutChangingHistoryOrIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New(t.TempDir())
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "keep this history"}}
	snapshot.Transcript = append([]types.Message(nil), snapshot.Messages...)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	before, err := manager.Load(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(dir(), snapshot.ID, transcriptFileName)
	history, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	// Prime the list cache before renaming.
	manager.List()
	if err := manager.Rename(snapshot.ID, "  修复登录 #auth  "); err != nil {
		t.Fatal(err)
	}
	after, err := manager.Load(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "修复登录 #auth" || manager.Current().Name != after.Name {
		t.Fatalf("name not retained: %+v", after)
	}
	after.Name = before.Name
	if !reflect.DeepEqual(before.Messages, after.Messages) || after.ID != before.ID || after.CreatedAt != before.CreatedAt || after.UpdatedAt != before.UpdatedAt {
		t.Fatal("rename changed conversation or identity")
	}
	now, _ := os.ReadFile(historyPath)
	if !bytes.Equal(history, now) {
		t.Fatal("rename rewrote transcript")
	}
	if listed := manager.List(); len(listed) != 1 || listed[0].Name != "修复登录 #auth" {
		t.Fatalf("stale list: %+v", listed)
	}
	// Normal saves must retain the active name.
	if err := manager.Save(manager.Current()); err != nil {
		t.Fatal(err)
	}
	if loaded, err := manager.Load(snapshot.ID); err != nil || loaded.Name != "修复登录 #auth" {
		t.Fatalf("save lost name: %+v %v", loaded, err)
	}
	other := manager.StartNew().ID
	if err := manager.Rename(snapshot.ID, ""); err != nil {
		t.Fatal(err)
	}
	if manager.CurrentID() != other {
		t.Fatal("renaming another session activated it")
	}
	if listed := manager.List(); listed[0].Name != "" {
		t.Fatal("clearing name did not refresh list")
	}
}

func TestRenameRejectsInvalidInputWithoutMutation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New(t.TempDir())
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "keep"}}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir(), snapshot.ID, "session.json")
	before, _ := os.ReadFile(path)
	for _, name := range []string{strings.Repeat("名", 81), "a\nb", "a\x1bb", string([]byte{0xff})} {
		if err := manager.Rename(snapshot.ID, name); err == nil {
			t.Errorf("accepted invalid name %q", name)
		}
	}
	for _, id := range []string{"../escape", "", "missing"} {
		if err := manager.Rename(id, "name"); err == nil {
			t.Errorf("accepted invalid session %q", id)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || manager.Current().Name != "" {
		t.Fatal("failed rename changed session")
	}
}

func TestSaveStaleSnapshotPreservesPersistedName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New(t.TempDir())
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "keep"}}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"new tag", ""} {
		stale := manager.Current()
		if err := manager.Rename(snapshot.ID, name); err != nil {
			t.Fatal(err)
		}
		stale.Messages = append(stale.Messages, types.Message{Role: "assistant", Content: "new history"})
		if err := manager.Save(stale); err != nil {
			t.Fatal(err)
		}
		loaded, err := manager.Load(snapshot.ID)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Name != name || manager.Current().Name != name || len(loaded.Messages) != len(stale.Messages) {
			t.Fatalf("stale save lost the name or new history: name=%q, messages=%d", loaded.Name, len(loaded.Messages))
		}
	}
}

func TestRenameRequiresRealSessionDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New(t.TempDir())
	id := manager.CurrentID()
	outside := t.TempDir()
	manifest := filepath.Join(outside, "session.json")
	original := []byte(`{"id":"` + id + `"}`)
	if err := os.WriteFile(manifest, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir(), id)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := manager.Rename(id, "updated"); err == nil {
		t.Fatal("rename accepted a symlinked session directory")
	}
	after, err := os.ReadFile(manifest)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("rename touched manifest outside session storage: %q, %v", after, err)
	}
}
