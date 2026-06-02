package session

import (
	"os"
	"testing"
	"time"
)

func TestNewSaveLoad(t *testing.T) {
	td := t.TempDir()
	t.Setenv("HOME", td) // override dir() to use temp dir

	s, err := New(td)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.ID == "" || s.CWD != td {
		t.Errorf("bad session: %+v", s)
	}

	// Save + Load.
	s.SystemPrompt = "test prompt"
	s.Messages = nil // reset for clean save
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(s.ID)
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

	s1, _ := New(td)
	time.Sleep(1 * time.Second)
	s2, _ := New(td)
	s1.UpdatedAt = time.Now().Unix()
	s2.UpdatedAt = time.Now().Add(-time.Hour).Unix()
	s1.Save()
	s2.Save()

	list := List()
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
		m := Meta{UpdatedAt: now + int64(tt.offset.Seconds())}
		if got := m.Age(); got != tt.want {
			t.Errorf("Age(%v) = %q, want %q", tt.offset, got, tt.want)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	td := t.TempDir()
	t.Setenv("HOME", td)
	_, err := Load("nonexistent")
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("expected NotExist error, got %v", err)
	}
}
