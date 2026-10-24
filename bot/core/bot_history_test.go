package core

import (
	"nekocode/util/fs"
	"os"
	"path/filepath"
	"testing"

	"nekocode/bot/provider/types"
)

func TestResumeKeepsOnlyActiveContextAndPendingHistory(t *testing.T) {
	b := newPersistTestBot(t)
	for i := 0; i < 1000; i++ {
		b.ctxMgr.Add("user", "old history")
	}
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	id := b.CurrentSessionID()
	base, tail, lazy := b.ctxMgr.TranscriptTail()
	if base != 1000 || len(tail) != 0 || !lazy {
		t.Fatalf("saved queue retained: base=%d tail=%d lazy=%v", base, len(tail), lazy)
	}
	// Keep only the model's required context in session.json, independently of
	// the complete transcript (as compaction does in production).
	snap := b.sess.Current()
	snap.Messages = []types.Message{{Role: "user", Content: "active context"}}
	if err := b.sess.Save(snap); err != nil {
		t.Fatal(err)
	}
	if _, err := b.resumeSession(id); err != nil {
		t.Fatal(err)
	}
	view := b.ctxMgr.Snapshot()
	if len(view.Transcript) != 0 || view.TranscriptBase != 1000 || len(view.Messages) > 5 {
		t.Fatalf("resume eagerly retained history: pending=%d base=%d active=%d", len(view.Transcript), view.TranscriptBase, len(view.Messages))
	}
	page, err := b.HistoryPage(0, 25, 1<<20)
	if err != nil || len(page.Messages) != 25 || page.Before != 976 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	b.ctxMgr.Add("user", "new message")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	persisted, err := b.sess.Load(id)
	if err != nil || len(persisted.Transcript) != 1001 || persisted.Transcript[1000].Content != "new message" {
		t.Fatalf("history lost on append: %v", err)
	}
}
func TestFailedSessionCommitRetriesWithoutDuplicatingHistory(t *testing.T) {
	b := newPersistTestBot(t)
	b.ctxMgr.Add("user", "first")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	id := b.CurrentSessionID()
	path := filepath.Join(fs.NekocodeDataDir("sessions"), id, "session.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	b.ctxMgr.Add("user", "second")
	if err := b.saveSession(); err == nil {
		t.Fatal("commit unexpectedly succeeded")
	}
	base, tail, _ := b.ctxMgr.TranscriptTail()
	if base != 1 || len(tail) != 1 {
		t.Fatal("failed save discarded pending history")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	b.ctxMgr.Add("user", "third")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	stored, err := b.sess.Load(id)
	if err != nil || len(stored.Transcript) != 3 || stored.Transcript[1].Content != "second" || stored.Transcript[2].Content != "third" {
		t.Fatalf("retry duplicated/lost data: %v", err)
	}
}
