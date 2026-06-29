package session

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/llm/types"
)

func TestFormatSessionList(t *testing.T) {
	if got := FormatSessionList(nil); got != "No saved sessions." {
		t.Fatalf("empty list = %q", got)
	}
	got := FormatSessionList([]Meta{{ID: "s1", CWD: "/tmp/work", MsgCount: 2}})
	if !strings.Contains(got, "s1") || !strings.Contains(got, "2 msgs") || !strings.Contains(got, "/sessions <id>") {
		t.Fatalf("unexpected list: %q", got)
	}
	if got := ResumeSuccess("s1", 3); !strings.Contains(got, "s1") || !strings.Contains(got, "3 messages") {
		t.Fatalf("unexpected resume success: %q", got)
	}
	if got := ResumeFailed("s1", errors.New("boom")); !strings.Contains(got, "Failed to resume session s1") {
		t.Fatalf("unexpected resume failure: %q", got)
	}
}

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
	if got := ExportSuccess(path, 1); !strings.Contains(got, path) || !strings.Contains(got, "1 messages") {
		t.Fatalf("unexpected export success: %q", got)
	}
	if got := ExportFailed(errors.New("write file: boom")); !strings.Contains(got, "Failed to write file") {
		t.Fatalf("unexpected export failure: %q", got)
	}
}

func TestApplyContextSnapshot(t *testing.T) {
	sess := &Snapshot{}
	snap := ctxmgr.ManagerSnapshot{
		SystemPrompt:    "sys",
		Skills:          "skills",
		Memory:          "mem",
		Archive:         "arch",
		Messages:        []types.Message{{Role: "user", Content: "hi"}},
		CompactBoundary: 3,
		Budget:          100,
	}
	ApplyContextSnapshot(sess, snap, 10, 20, map[string]bool{"b": true, "a": true, "skip": false})

	if sess.SystemPrompt != "sys" || sess.ContextWindow != 100 || sess.PromptTokens != 10 || sess.CompletionTokens != 20 {
		t.Fatalf("session fields not applied: %+v", sess)
	}
	if !reflect.DeepEqual(sess.LoadedSkills, []string{"a", "b"}) {
		t.Fatalf("loaded skills = %+v", sess.LoadedSkills)
	}
}

func TestManagerSnapshot(t *testing.T) {
	sess := &Snapshot{
		SystemPrompt:    "sys",
		Skills:          "skills",
		Memory:          "mem",
		Archive:         "arch",
		CompactBoundary: 2,
		ContextWindow:   50,
	}
	got := ManagerSnapshot(sess)
	if got.SystemPrompt != "sys" || got.Skills != "skills" || got.Budget != 50 || got.CompactBoundary != 2 {
		t.Fatalf("snapshot mismatch: %+v", got)
	}
}
