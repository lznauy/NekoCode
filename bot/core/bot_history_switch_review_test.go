package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/util/fs"
)

func TestFailedHistoryCommitSurvivesSwitchExportAndAppend(t *testing.T) {
	b := newPersistTestBot(t)
	b.ctxMgr.Add("user", "first")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	id := b.CurrentSessionID()
	path := filepath.Join(fs.NekocodeDataDir("sessions"), id, "session.json")
	metadata, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	b.ctxMgr.Add("user", "second")
	if err := b.saveSession(); err == nil {
		t.Fatal("expected failed metadata commit")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, metadata, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	b.ctxMgr.Add("user", "unrelated")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	if err := b.ResumeSession(id); err != nil {
		t.Fatal(err)
	}
	base, tail, _ := b.ctxMgr.TranscriptTail()
	if base != 2 || len(tail) != 0 {
		t.Fatalf("recovered base=%d pending=%d", base, len(tail))
	}
	b.ctxMgr.Add("user", "third")
	export := filepath.Join(t.TempDir(), "history.json")
	base, tail, _ = b.ctxMgr.TranscriptTail()
	_, count, err := b.sess.ExportHistory(base, tail, export)
	if err != nil || count != 3 {
		t.Fatalf("export count=%d err=%v", count, err)
	}
	data, err := os.ReadFile(export)
	if err != nil {
		t.Fatal(err)
	}
	var exported []types.Message
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"first", "second", "third"} {
		if exported[i].Content != want {
			t.Fatalf("export[%d]=%q want %q", i, exported[i].Content, want)
		}
	}
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	stored, err := b.sess.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Transcript) != 3 {
		t.Fatalf("stored transcript=%d", len(stored.Transcript))
	}
}
