package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewAtPinsIndependentProjectsWithoutChdir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{t.TempDir(), t.TempDir()} {
		bot, err := NewAt(root)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := bot.Close(); err != nil {
				t.Error(err)
			}
		})
		if bot.cwd != root || bot.toolbox.Workspace().Primary() != root {
			t.Fatalf("bot escaped project %q", root)
		}
		path, _, allowed, err := bot.toolbox.Workspace().CheckWrite("file.txt")
		if err != nil || !allowed || path != filepath.Join(root, "file.txt") {
			t.Fatalf("path=%q allowed=%v err=%v", path, allowed, err)
		}
	}
	after, err := os.Getwd()
	if err != nil || after != before {
		t.Fatalf("cwd changed: %q -> %q err=%v", before, after, err)
	}
}
