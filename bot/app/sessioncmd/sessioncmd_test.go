package sessioncmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/llm/types"
	"nekocode/bot/session"
)

func TestFormatSessionList(t *testing.T) {
	if got := FormatSessionList(nil); got != "No saved sessions." {
		t.Fatalf("empty list = %q", got)
	}
	got := FormatSessionList([]session.Meta{{ID: "s1", CWD: "/tmp/work", MsgCount: 2}})
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
