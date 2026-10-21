package session

import (
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewSaveLoad(t *testing.T) {
	td := t.TempDir()
	t.Setenv("HOME", td) // override dir() to use temp dir

	s := newSnapshot(td)
	if s.ID == "" || s.CWD != td {
		t.Errorf("bad session: %+v", s)
	}

	// Save + Load.
	s.SystemPrompt = "test prompt"
	s.Messages = nil // reset for clean save
	if err := s.save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(dir(), s.ID, "session.json")
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("session mode after save = %v, %v; want 600", info, err)
	}

	loaded, err := load(s.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SystemPrompt != "test prompt" {
		t.Errorf("loaded prompt = %q", loaded.SystemPrompt)
	}

}

func TestList(t *testing.T) {
	td := t.TempDir()
	t.Setenv("HOME", td)

	s1 := newSnapshot(td)
	time.Sleep(1 * time.Second)
	s2 := newSnapshot(td)
	s1.UpdatedAt = time.Now().Unix()
	s2.UpdatedAt = time.Now().Add(-time.Hour).Unix()
	s1.save()
	s2.save()

	list := list()
	if len(list) < 2 {
		t.Fatalf("expected >= 2 sessions, got %d", len(list))
	}
	if list[0].UpdatedAt < list[1].UpdatedAt {
		t.Error("list not sorted by UpdatedAt desc")
	}
}

func TestMetaAge(t *testing.T) {
	now := time.Now().Unix()
	tests := []struct {
		offset time.Duration
		want   string
	}{
		{0, "just now"},
		{-30 * time.Second, "just now"},
		{-2 * time.Minute, "2m ago"},
		{-3 * time.Hour, "3h ago"},
		{-48 * time.Hour, "2d ago"},
	}
	for _, tt := range tests {
		m := Meta{CreatedAt: now + int64(tt.offset.Seconds())}
		if got := m.Age(); got != tt.want {
			t.Errorf("Age(%v) = %q, want %q", tt.offset, got, tt.want)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	td := t.TempDir()
	t.Setenv("HOME", td)
	_, err := load("nonexistent")
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("expected NotExist error, got %v", err)
	}
}

func TestSessionIDValidationProtectsSessionDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newSnapshot("/tmp/work")
	if err := s.save(); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"", ".", "..", "../escape", "nested/session"} {
		if _, err := load(id); err == nil {
			t.Fatalf("Load accepted unsafe id %q", id)
		}
		if err := deleteSnapshot(id); err == nil {
			t.Fatalf("Delete accepted unsafe id %q", id)
		}
	}
	if _, err := load(s.ID); err != nil {
		t.Fatalf("unsafe delete removed valid session: %v", err)
	}

	s.ID = "../escape"
	if err := s.save(); err == nil {
		t.Fatal("Save accepted an unsafe snapshot id")
	}
}

func TestLoadRejectsMismatchedSnapshotID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newSnapshot("/tmp/work")
	requestedID := s.ID
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir(), requestedID, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), requestedID, "../escape", 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := load(requestedID); err == nil {
		t.Fatal("Load accepted a mismatched snapshot id")
	}
}

func TestToolOutputsFollowSessionDeletion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := ToolOutputDir("session-one")
	if err != nil {
		t.Fatal(err)
	}
	path, err := toolcore.NewOutputStore(dir).PreserveOutput("retained output")
	if err != nil {
		t.Fatal(err)
	}
	otherDir, _ := ToolOutputDir("session-two")
	otherPath, err := toolcore.NewOutputStore(otherDir).PreserveOutput("other output")
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteSnapshot("session-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("session deletion left its tool output")
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatal("session deletion removed another session's output")
	}
	if _, err := ToolOutputDir("../escape"); err == nil {
		t.Fatal("accepted invalid session id")
	}
}
