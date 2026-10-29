package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
	"nekocode/util/attachment"
)

func TestForkCopiesCompactedHistoryAndOwnedAttachments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := New(t.TempDir())
	source := manager.Current()
	tools, _ := ToolOutputDir(source.ID)
	images, _ := attachment.ImageDir(source.ID)
	for _, dir := range []string{tools, images} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	artifact := filepath.Join(tools, "result.txt")
	image := filepath.Join(images, "paste.png")
	for path, data := range map[string]string{artifact: "long tool output", image: "image fixture"} {
		if e := os.WriteFile(path, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	source.Transcript = []types.Message{{Role: "user", Content: "old question", Images: []types.MessageImage{{Path: image, MIME: "image/png"}}}, {Role: "tool", Content: "output " + artifact, DeferredContent: artifact, ToolOutput: protocol.ToolOutputArtifact{Path: artifact}}, {Role: "assistant", Content: "answer"}}
	source.Messages = cloneMessages(source.Transcript[1:])
	source.Archive = "earlier compacted context: " + artifact
	source.Memory = "attached image " + image
	source.CompactCount = 1
	source.CheckpointTurns = []string{"old-turn"}
	source.CheckpointNext = 2
	source.CompletionTokens = 99
	source.CacheHitTokens = 50
	source.LoadedSkills = []string{"check"}
	if e := manager.Save(source); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(filepath.Join(dir(), source.ID, "session.json"))
	if e != nil {
		t.Fatal(e)
	}
	fork, e := manager.Fork(t.Context(), source.ID)
	if e != nil {
		t.Fatal(e)
	}
	if manager.CurrentID() != source.ID {
		t.Fatal("fork activated before caller was ready")
	}
	if fork.ID == source.ID || fork.ForkedFrom != source.ID || fork.ForkedAtSeq != 3 || !strings.HasPrefix(fork.Archive, "earlier compacted context:") || len(fork.Messages) != 2 || len(fork.Transcript) != 3 {
		t.Fatalf("fork state: %+v", fork)
	}
	if fork.CompletionTokens != 0 || fork.CacheHitTokens != 0 || len(fork.CheckpointTurns) != 0 || fork.CheckpointNext != 0 {
		t.Fatal("inherited counters or file rollback authority")
	}
	after, _ := os.ReadFile(filepath.Join(dir(), source.ID, "session.json"))
	if string(before) != string(after) {
		t.Fatal("source metadata changed")
	}
	if fork.Transcript[0].Images[0].Path == image || fork.Messages[0].ToolOutput.Path == artifact {
		t.Fatal("attachments still owned by source")
	}
	if !strings.Contains(fork.Archive, fork.Messages[0].ToolOutput.Path) || !strings.Contains(fork.Memory, fork.Transcript[0].Images[0].Path) {
		t.Fatal("compacted context retains source-owned paths")
	}
	if !strings.Contains(fork.Messages[0].Content, fork.Messages[0].ToolOutput.Path) || fork.Messages[0].DeferredContent != fork.Messages[0].ToolOutput.Path {
		t.Fatal("rendered reference not remapped")
	}
	if e = manager.Delete(source.ID); e != nil {
		t.Fatal(e)
	}
	if e = attachment.DeleteSession(source.ID); e != nil {
		t.Fatal(e)
	}
	restored, e := manager.Load(fork.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{restored.Transcript[0].Images[0].Path, restored.Messages[0].ToolOutput.Path} {
		if _, e = os.ReadFile(path); e != nil {
			t.Fatal("branch depends on deleted source", e)
		}
	}
	restored.Messages = append(restored.Messages, types.Message{Role: "user", Content: "branch continuation"})
	restored.Transcript = append(restored.Transcript, types.Message{Role: "user", Content: "branch continuation"})
	if e = manager.Save(restored); e != nil {
		t.Fatal(e)
	}
	loaded, e := manager.Load(restored.ID)
	if e != nil || len(loaded.Transcript) != 4 {
		t.Fatalf("continued branch: %+v %v", loaded, e)
	}
}
func TestForkRejectsEmptyAndCancelled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(t.TempDir())
	s := m.Current()
	// Persist directly because Manager.Save deliberately removes empty sessions.
	if e := s.save(); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Fork(t.Context(), s.ID); e == nil {
		t.Fatal("forked empty session")
	}
	s.Messages = []types.Message{{Role: "user", Content: "hello"}}
	s.Transcript = cloneMessages(s.Messages)
	if e := m.Save(s); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := m.Fork(ctx, s.ID); e != context.Canceled {
		t.Fatalf("cancel: %v", e)
	}
	if list := m.List(); len(list) != 1 {
		t.Fatalf("unexpected partial fork: %+v", list)
	}
}
func TestForkAttachmentFailureLeavesSourceAndCleansPartialCopy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(t.TempDir())
	s := m.Current()
	images, _ := attachment.ImageDir(s.ID)
	if e := os.MkdirAll(images, 0700); e != nil {
		t.Fatal(e)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	if e := os.WriteFile(secret, []byte("must not copy"), 0600); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(images, "escape.png")
	if e := os.Symlink(secret, path); e != nil {
		t.Skip(e)
	}
	s.Messages = []types.Message{{Role: "user", Content: "question", Images: []types.MessageImage{{Path: path}}}}
	s.Transcript = cloneMessages(s.Messages)
	if e := m.Save(s); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Fork(t.Context(), s.ID); e == nil {
		t.Fatal("copied symlink outside owning image directory")
	}
	if m.CurrentID() != s.ID {
		t.Fatal("switched after failed copy")
	}
	entries, e := os.ReadDir(dir())
	if e != nil || len(entries) != 1 {
		t.Fatalf("partial session leaked: %v %v", entries, e)
	}
	entries, e = os.ReadDir(filepath.Dir(images))
	if e != nil || len(entries) != 1 {
		t.Fatalf("partial images leaked: %v %v", entries, e)
	}
	if _, e = m.Load(s.ID); e != nil {
		t.Fatal("source damaged", e)
	}
}

func TestForkFromLazyCompactedSessionPreservesFullHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(t.TempDir())
	s := m.Current()
	for i := 0; i < 1000; i++ {
		s.Transcript = append(s.Transcript, types.Message{Role: "user", Content: strings.Repeat("history ", 10)})
	}
	s.Messages = cloneMessages(s.Transcript[990:])
	s.Archive = "compacted earlier 990 messages"
	if e := m.Save(s); e != nil {
		t.Fatal(e)
	}
	lazy, e := m.LoadLazy(s.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Activate(lazy); e != nil {
		t.Fatal(e)
	}
	fork, e := m.Fork(t.Context(), s.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(fork.Transcript) != 1000 || len(fork.Messages) != 10 || fork.Archive != s.Archive {
		t.Fatal("fork copied only active context or expanded compacted context")
	}
	fork.Messages = append(fork.Messages, types.Message{Role: "user", Content: "new branch message"})
	fork.Transcript = append(fork.Transcript, types.Message{Role: "user", Content: "new branch message"})
	if e = m.Save(fork); e != nil {
		t.Fatal(e)
	}
	original, e := m.Load(s.ID)
	if e != nil || len(original.Transcript) != 1000 {
		t.Fatal("fork append changed source history", e)
	}
}
