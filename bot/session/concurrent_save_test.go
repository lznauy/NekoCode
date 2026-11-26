package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nekocode/bot/provider/types"
)

func isolateSaveTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestStaleSaveCannotReplaceGoalOrAppendHistory(t *testing.T) {
	isolateSaveTestHome(t)
	first := New(t.TempDir())
	other := New(first.cwd)
	snapshot := first.Current()
	snapshot.Goal = &Goal{Status: "active"}
	snapshot.Transcript = []types.Message{{Role: "user", Content: "initial"}}
	if err := first.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	stale, err := other.LoadLazy(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Goal.Status = "paused"
	if err := first.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	stale.Transcript = []types.Message{{Role: "user", Content: "stale"}}
	if err := other.Save(stale); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("stale save: %v", err)
	}
	loaded, err := other.Load(snapshot.ID)
	if err != nil || loaded.Goal.Status != "paused" || len(loaded.Transcript) != 1 {
		t.Fatalf("committed goal or transcript changed: %+v, %v", loaded, err)
	}
}

func TestSaveConflictDoesNotDeleteOrResurrectSession(t *testing.T) {
	isolateSaveTestHome(t)
	manager := New(t.TempDir())
	draft := manager.Current()
	session := draft.Clone()
	session.Goal = &Goal{Status: "active"}
	if err := manager.Save(session); err != nil {
		t.Fatal(err)
	}
	if err := manager.SavePreservingDraft(draft); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("stale draft save: %v", err)
	}
	if err := manager.Delete(session.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(session); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("deleted session save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir(), session.ID)); !os.IsNotExist(err) {
		t.Fatalf("deleted session directory: %v", err)
	}
}

func TestConcurrentSessionWritersCommitOneTurn(t *testing.T) {
	isolateSaveTestHome(t)
	first := New(t.TempDir())
	snapshot := first.Current()
	snapshot.Goal = &Goal{Status: "active"}
	if err := first.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	var writers []*Manager
	var copies []*Snapshot
	for _, name := range []string{"first", "second"} {
		manager := New(first.cwd)
		s, err := manager.Load(snapshot.ID)
		if err != nil {
			t.Fatal(err)
		}
		s.Transcript = []types.Message{{Role: "user", Content: name}}
		writers = append(writers, manager)
		copies = append(copies, s)
	}
	start := make(chan struct{})
	results := make(chan error, len(writers))
	for i, manager := range writers {
		go func() { <-start; results <- manager.Save(copies[i]) }()
	}
	close(start)
	ok := 0
	for range writers {
		if err := <-results; err == nil {
			ok++
		} else if !errors.Is(err, ErrSessionConflict) {
			t.Fatal(err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d writers succeeded", ok)
	}
	s, err := first.Load(snapshot.ID)
	if err != nil || len(s.Transcript) != 1 || !slices.Contains([]string{"first", "second"}, s.Transcript[0].Content) {
		t.Fatalf("unexpected history: %+v %v", s, err)
	}
}

func TestLegacySessionReceivesRevisionOnFirstChange(t *testing.T) {
	isolateSaveTestHome(t)
	manager := New(t.TempDir())
	snapshot := manager.Current()
	snapshot.Goal = &Goal{Status: "active"}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir(), snapshot.ID, "session.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "revision")
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	old, err := manager.Load(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := old.Clone()
	old.Goal.Status = "paused"
	if err := manager.Save(old); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(before); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("save from pre-upgrade snapshot: %v", err)
	}
}

func TestManifestWriteFailurePreservesRetryableTranscript(t *testing.T) {
	for _, lazy := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "lazy"}[lazy], func(t *testing.T) {
			isolateSaveTestHome(t)
			manager := New(t.TempDir())
			seed := manager.Current()
			seed.Messages = []types.Message{{Role: "user", Content: "first"}}
			seed.Transcript = types.CloneMessages(seed.Messages)
			if err := manager.Save(seed); err != nil {
				t.Fatal(err)
			}
			writer, err := loadMode(seed.ID, lazy)
			if err != nil {
				t.Fatal(err)
			}
			stale := writer.Clone()
			appendTurn := func(s *Snapshot, text string) {
				msg := types.Message{Role: "assistant", Content: text}
				s.Messages = append(s.Messages, msg)
				s.Transcript = append(s.Transcript, msg)
			}
			appendTurn(writer, "pending")
			appendTurn(stale, "other")
			// Fail after the journal append and before the next manifest commit.
			slot := messagesPath(filepath.Join(dir(), seed.ID), 1)
			if err := os.Mkdir(slot, 0700); err != nil {
				t.Fatal(err)
			}
			if err := manager.Save(writer); err == nil {
				t.Fatal("expected slot write to fail")
			}
			if err := os.Remove(slot); err != nil {
				t.Fatal(err)
			}
			if err := manager.Save(stale); err == nil {
				t.Fatal("replaced an uncommitted journal suffix")
			}
			changed := writer.Clone()
			changed.Transcript[len(changed.Transcript)-1].Content = "edited pending"
			if err := manager.Save(changed); err == nil {
				t.Fatal("retry acknowledged an edited uncommitted turn")
			}
			if err := manager.Save(writer); err != nil {
				t.Fatal(err)
			}
			actual, err := manager.Load(seed.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.EqualFunc(actual.Transcript, writer.Messages, func(a, b types.Message) bool { return a.Content == b.Content }) || actual.CommittedTranscriptSeq != 2 {
				t.Fatalf("retry history=%+v, committed=%d", actual.Transcript, actual.CommittedTranscriptSeq)
			}
		})
	}
}

func TestMissingManifestOnlyAllowsLastWriterToRecover(t *testing.T) {
	isolateSaveTestHome(t)
	manager := New(t.TempDir())
	writer := manager.Current()
	writer.Goal = &Goal{Status: "active"}
	if err := manager.Save(writer); err != nil {
		t.Fatal(err)
	}
	stale := writer.Clone()
	writer.Goal.Status = "paused"
	if err := manager.Save(writer); err != nil {
		t.Fatal(err)
	}
	if err := manager.Rename(writer.ID, "latest name"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir(), writer.ID, "session.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stale.Transcript = []types.Message{{Role: "user", Content: "stale turn"}}
	if err := manager.Save(stale); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("outdated writer recreated the manifest: %v", err)
	}
	if err := manager.Save(writer); err != nil {
		t.Fatal(err)
	}
	loaded, err := manager.Load(writer.ID)
	if err != nil || loaded.Name != "latest name" || loaded.Goal.Status != "paused" || len(loaded.Transcript) != 0 {
		t.Fatalf("recovered manifest: %+v, %v", loaded, err)
	}
}

func TestDraftCannotDiscardPendingJournal(t *testing.T) {
	isolateSaveTestHome(t)
	manager := New(t.TempDir())
	writer := manager.Current()
	writer.Goal = &Goal{Status: "active"}
	if err := manager.Save(writer); err != nil {
		t.Fatal(err)
	}
	draft := writer.Clone()
	draft.Goal = nil
	writer.Messages = []types.Message{{Role: "user", Content: "pending turn"}}
	writer.Transcript = types.CloneMessages(writer.Messages)
	slot := messagesPath(filepath.Join(dir(), writer.ID), 1)
	if err := os.Mkdir(slot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(writer); err == nil {
		t.Fatal("expected blocked message slot")
	}
	if err := manager.SavePreservingDraft(draft); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("draft discarded outstanding history: %v", err)
	}
	loaded, err := manager.Load(writer.ID)
	if err != nil || len(loaded.Transcript) != 1 || loaded.Transcript[0].Content != "pending turn" {
		t.Fatalf("failed to recover pending turn: %+v, %v", loaded, err)
	}
}

func TestMissingManifestRecoveryRewritesMissingMessages(t *testing.T) {
	isolateSaveTestHome(t)
	manager := New(t.TempDir())
	writer := manager.Current()
	writer.Messages = []types.Message{{Role: "user", Content: "recover this turn"}}
	writer.Transcript = types.CloneMessages(writer.Messages)
	if err := manager.Save(writer); err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(dir(), writer.ID)
	if err := os.Remove(filepath.Join(d, "session.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(messagesPath(d, 0)); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(writer); err != nil {
		t.Fatal(err)
	}
	restored, err := manager.Load(writer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Messages) != 1 || restored.Messages[0].Content != "recover this turn" {
		t.Fatalf("recovered messages = %+v", restored.Messages)
	}
}
