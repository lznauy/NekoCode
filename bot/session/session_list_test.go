package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkSessionList(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	for i := 0; i < 1000; i++ {
		s := newSnapshot("/workspace")
		s.ID = fmt.Sprintf("session-%04d", i)
		s.SystemPrompt = strings.Repeat("prompt ", 10000)
		s.Archive = strings.Repeat("history ", 10000)
		if err := s.save(); err != nil {
			b.Fatal(err)
		}
	}
	// Measure normal repeated opening of the picker, excluding fixture setup.
	if len(list()) != 1000 {
		b.Fatal("missing sessions")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(list()) != 1000 {
			b.Fatal("missing sessions")
		}
	}
}

func TestSessionListProjectionSavedAndRefreshed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newSnapshot("/workspace")
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir(), s.ID, "list-meta.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("save must prepare compact list metadata: %v", err)
	}
	// A separately written manifest must invalidate the projection immediately.
	manifest := filepath.Join(dir(), s.ID, "session.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["cwd"] = json.RawMessage(`"/different-workspace"`)
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, data, 0600); err != nil {
		t.Fatal(err)
	}
	items := list()
	if len(items) != 1 || items[0].CWD != "/different-workspace" {
		t.Fatalf("stale metadata: %+v", items)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if len(list()) != 1 {
		t.Fatal("old sessions without projection must remain visible")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("legacy metadata was not cached: %v", err)
	}
	if err := os.WriteFile(p, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if len(list()) != 1 {
		t.Fatal("broken projection must fall back to manifest")
	}
	if err := deleteSnapshot(s.ID); err != nil {
		t.Fatal(err)
	}
	if len(list()) != 0 {
		t.Fatal("deleted session remains listed")
	}
}

func TestSessionListProjectionDoesNotRecreateDeletedDirectory(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "deleted-session")
	if err := os.Mkdir(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	// The source was read, then deletion completed before the cache write.
	if err := os.RemoveAll(sessionDir); err != nil {
		t.Fatal(err)
	}
	err := writeListProjection(filepath.Join(sessionDir, "list-meta.json"), []byte(`{}`))
	if err == nil {
		t.Fatal("cache write must fail when the session directory was deleted")
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("cache recreated deleted directory: %v", err)
	}
}

func TestSessionListPreservesPrivateManifestPermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newSnapshot("/workspace")
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir(), s.ID, "session.json")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMeta(s.ID); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("list left manifest permissions at %o, want 600", info.Mode().Perm())
	}
}
