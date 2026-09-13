package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/provider/types"
)

func TestExportMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctx.json")
	gotPath, err := ExportMessages([]types.Message{{Role: "user", Content: "hi"}}, path)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path {
		t.Fatalf("path = %q", gotPath)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"role": "user"`) {
		t.Fatalf("unexpected export: %s", data)
	}
}

func TestSnapshotCaptureContext(t *testing.T) {
	sess := &Snapshot{}
	snap := ctxmgr.ManagerSnapshot{
		SystemPrompt: "sys",
		Skills:       "skills",
		Memory:       "mem",
		Archive:      "arch",
		Messages: []types.Message{
			{Role: "user", Content: "hi"},
			{Role: "user", Content: "<workspace_event/>", Source: types.MessageSourceRuntimeEvent},
		},
		Budget: 100,
		Tracker: token.State{
			LastPromptTokens: 1000,
			NewMessageTokens: 50,
			CacheHitTokens:   70,
			CacheMissTokens:  30,
			Sub: token.SubStats{
				Count:           2,
				TotalTokens:     500,
				CacheHitTokens:  40,
				CacheMissTokens: 60,
			},
		},
		PrefixBaseline: ctxmgr.PrefixBaseline{System: "syshash", Tools: "toolshash"},
	}
	sess.CaptureContext(snap, 10, 20, map[string]bool{"b": true, "a": true, "skip": false})

	if sess.SystemPrompt != "sys" || sess.ContextWindow != 100 || sess.PromptTokens != 10 || sess.CompletionTokens != 20 {
		t.Fatalf("session fields not applied: %+v", sess)
	}
	if !reflect.DeepEqual(sess.LoadedSkills, []string{"a", "b"}) {
		t.Fatalf("loaded skills = %+v", sess.LoadedSkills)
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Source != types.MessageSourceRuntimeEvent {
		t.Fatalf("runtime event was not captured: %+v", sess.Messages)
	}
	if sess.TrackerPrompt != 1000 || sess.TrackerCompletion != 0 || sess.TrackerNewTokens != 50 || sess.CacheHitTokens != 70 || sess.CacheMissTokens != 30 || sess.SubCount != 2 || sess.SubTokens != 500 || sess.SubCacheHit != 40 || sess.SubCacheMiss != 60 {
		t.Fatalf("tracker fields not applied: %+v", sess)
	}
	if sess.PrefixSystemHash != "syshash" || sess.PrefixToolsHash != "toolshash" {
		t.Fatalf("prefix baseline not persisted: %+v", sess)
	}
}

func TestSnapshotContextSnapshot(t *testing.T) {
	sess := &Snapshot{
		SystemPrompt:    "sys",
		Skills:          "skills",
		Memory:          "mem",
		Archive:         "arch",
		CompactBoundary: 2,
		Messages: []types.Message{
			{Role: "user", Content: "hidden one"},
			{Role: "assistant", Content: "hidden two"},
			{Role: "user", Content: "visible"},
		},
		ContextWindow:     50,
		TrackerPrompt:     1000,
		TrackerCompletion: 200,
		TrackerNewTokens:  50,
		CacheHitTokens:    70,
		CacheMissTokens:   30,
		SubCount:          2,
		SubTokens:         500,
		SubCacheHit:       40,
		SubCacheMiss:      60,

		PrefixSystemHash: "syshash",
		PrefixToolsHash:  "toolshash",
	}
	got := sess.ContextSnapshot()
	if got.SystemPrompt != "sys" || got.Skills != "skills" || got.Budget != 50 {
		t.Fatalf("snapshot mismatch: %+v", got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "visible" {
		t.Fatalf("legacy boundary was not normalized: %+v", got.Messages)
	}
	if got.Tracker.CacheHitTokens != 70 || got.Tracker.CacheMissTokens != 30 || got.Tracker.Sub.Count != 2 || got.Tracker.Sub.TotalTokens != 500 {
		t.Fatalf("tracker mismatch: %+v", got.Tracker)
	}
	if got.Tracker.LastPromptTokens != 1000 || got.Tracker.NewMessageTokens != 50 {
		t.Fatalf("tracker token state mismatch: %+v", got.Tracker)
	}
	if got.PrefixBaseline.System != "syshash" || got.PrefixBaseline.Tools != "toolshash" {
		t.Fatalf("prefix baseline mismatch: %+v", got.PrefixBaseline)
	}
}

func TestManagerSaveRemovesEmptySession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New("/tmp/work")
	snapshot := m.Current()
	id := m.CurrentID()

	if err := m.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := load(id); !os.IsNotExist(err) {
		t.Fatalf("empty session should be removed, load err = %v", err)
	}
	if got := m.CurrentID(); got != "" {
		t.Fatalf("current id after empty session removal = %q, want empty", got)
	}

	snapshot = m.StartNew()
	snapshot.Messages = []types.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	if err := m.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	nextID := m.CurrentID()
	if nextID == "" {
		t.Fatal("current id after non-empty save is empty")
	}
	loaded, err := load(nextID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("saved messages = %d, want 2", len(loaded.Messages))
	}
}

// The prefix diagnostics must survive a real save/load, or a resumed process
// cannot tell whether it changed the cached head, and /context would blank the
// per-turn figures.
func TestManagerPersistsPrefixDiagnostics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New("/tmp/work")
	snapshot := m.StartNew()
	messages := []types.Message{{Role: "user", Content: "hello"}}
	snapshot.CaptureContext(ctxmgr.ManagerSnapshot{
		SystemPrompt:   "sys",
		Messages:       messages,
		PrefixBaseline: ctxmgr.PrefixBaseline{System: "syshash", Tools: "toolshash"},
		PrefixTurn: &ctxmgr.PrefixTurnStats{
			Requests: 2, HitTokens: 5, MissTokens: 7,
			PeakMiss: ctxmgr.PrefixCallStats{Request: 1, MissTokens: 7, Parts: []string{"history"}},
		},
		CompactCount: 2,
		TrimCount:    40,
	}, 0, 0, nil)

	if err := m.Save(snapshot); err != nil {
		t.Fatal(err)
	}

	loaded, err := load(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.ContextSnapshot()
	if got.PrefixBaseline.System != "syshash" || got.PrefixBaseline.Tools != "toolshash" {
		t.Fatalf("baseline did not survive save/load: %+v", got.PrefixBaseline)
	}
	if got.PrefixTurn == nil || got.PrefixTurn.Requests != 2 || got.PrefixTurn.HitTokens != 5 || got.PrefixTurn.MissTokens != 7 {
		t.Fatalf("turn did not survive save/load: %+v", got.PrefixTurn)
	}
	if parts := got.PrefixTurn.PeakMiss.Parts; len(parts) != 1 || parts[0] != "history" {
		t.Fatalf("turn attribution lost: %+v", got.PrefixTurn.PeakMiss)
	}
	// The archive counters must come back with the archive itself.
	if got.CompactCount != 2 || got.TrimCount != 40 {
		t.Fatalf("archive counters did not survive save/load: %d/%d", got.CompactCount, got.TrimCount)
	}

	// Pin the on-disk keys: renaming a Go field must not silently invalidate
	// sessions saved by an earlier build.
	raw, err := os.ReadFile(filepath.Join(dir(), snapshot.ID, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"prefix_turn"`, `"requests"`, `"hit_tokens"`, `"miss_tokens"`, `"peak_miss"`, `"lowest_hit"`, `"compact_count"`, `"trim_count"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("session.json is missing %s: %s", key, raw)
		}
	}
	if strings.Contains(string(raw), `"Requests"`) || strings.Contains(string(raw), `"PeakMiss"`) {
		t.Errorf("session.json leaked Go field names: %s", raw)
	}
}

func TestManagerStartNewAndClearCurrent(t *testing.T) {
	m := New("/tmp/work")
	oldID := m.CurrentID()

	next := m.StartNew()
	if next.ID == "" || next.ID == oldID || m.CurrentID() != next.ID {
		t.Fatalf("new session = %#v, current = %q, old = %q", next, m.CurrentID(), oldID)
	}

	m.ClearCurrent()
	if m.CurrentID() != "" {
		t.Fatalf("current session after clear = %q", m.CurrentID())
	}
}

func TestManagerLoadDoesNotChangeCurrentSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seed := New("/tmp/work")
	target := seed.Current()
	target.Messages = []types.Message{{Role: "user", Content: "saved"}}
	if err := seed.Save(target); err != nil {
		t.Fatal(err)
	}

	manager := New("/tmp/work")
	currentID := manager.CurrentID()
	loaded, err := manager.Load(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != target.ID {
		t.Fatalf("loaded id = %q, want %q", loaded.ID, target.ID)
	}
	if manager.CurrentID() != currentID {
		t.Fatalf("load changed current id to %q, want %q", manager.CurrentID(), currentID)
	}
	if err := manager.Activate(loaded); err != nil {
		t.Fatal(err)
	}
	if manager.CurrentID() != target.ID {
		t.Fatalf("activate current id = %q, want %q", manager.CurrentID(), target.ID)
	}
}
