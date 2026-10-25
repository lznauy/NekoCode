package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func seedToolArtifact(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	seedObservedSubagent(t, []types.Message{{Role: "user", Content: "inspect"}, {Role: "tool", ToolCallID: "call", Content: "preview", ToolOutput: protocol.ToolOutputArtifact{Path: path}}})
}

func TestSubagentToolOutputPagesUTF8WithoutLosingContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, _ := ToolOutputDir("parent")
	path := filepath.Join(root, "subagents", "child", "result.txt")
	content := strings.Repeat("猫与测试日志\n", 12000)
	seedToolArtifact(t, path, content)
	var got strings.Builder
	offset := int64(0)
	for {
		page, err := SubagentToolOutput("parent", "child", 2, offset)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Content) > SubagentOutputBytes || !utf8.ValidString(page.Content) || (page.HasMore && page.NextOffset <= offset) {
			t.Fatalf("bad output cursor: %+v", page)
		}
		got.WriteString(page.Content)
		offset = page.NextOffset
		if !page.HasMore {
			break
		}
	}
	if got.String() != content {
		t.Fatal("paged original output lost content")
	}
	for _, args := range []struct {
		sequence int
		offset   int64
	}{{0, 0}, {3, 0}, {2, -1}, {2, int64(len(content) + 1)}} {
		if _, err := SubagentToolOutput("parent", "child", args.sequence, args.offset); err == nil {
			t.Fatalf("accepted invalid cursor: %+v", args)
		}
	}
}

func TestSubagentToolOutputInvalidUTF8StillAdvances(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, _ := ToolOutputDir("parent")
	path := filepath.Join(root, "subagents", "child", "result.txt")
	seedToolArtifact(t, path, strings.Repeat(string([]byte{0x80}), SubagentOutputBytes*2))
	page, err := SubagentToolOutput("parent", "child", 2, 0)
	if err != nil || page.NextOffset <= 0 || !page.HasMore {
		t.Fatalf("output cursor stuck: %+v %v", page, err)
	}
}

func TestSubagentToolOutputRejectsOtherActorAndSymlinks(t *testing.T) {
	for _, mode := range []string{"other_actor", "file_symlink", "directory_symlink"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			root, _ := ToolOutputDir("parent")
			path := filepath.Join(root, "subagents", "child", "result.txt")
			if mode == "other_actor" {
				path = filepath.Join(root, "subagents", "other", "result.txt")
			}
			seedToolArtifact(t, path, "private output")
			if mode == "file_symlink" {
				outside := filepath.Join(t.TempDir(), "private.txt")
				os.WriteFile(outside, []byte("outside"), 0600)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if mode == "directory_symlink" {
				outside := t.TempDir()
				os.WriteFile(filepath.Join(outside, "result.txt"), []byte("outside"), 0600)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Dir(path)); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if _, err := SubagentToolOutput("parent", "child", 2, 0); err == nil {
				t.Fatalf("read forbidden artifact: %s", mode)
			}
		})
	}
}
