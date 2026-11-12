package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteSubagentRejectsOutputDirectorySymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, _ := subagentRoot("parent")
	if err := os.MkdirAll(filepath.Join(root, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	sentinel := filepath.Join(external, "child", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	outputs, _ := ToolOutputDir("parent")
	if err := os.MkdirAll(outputs, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(outputs, "subagents")); err != nil {
		t.Skip(err)
	}
	if err := DeleteSubagent("parent", "child"); err == nil {
		t.Error("deletion through output-directory symlink accepted")
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep" {
		t.Fatalf("external output changed: %q %v", got, err)
	}
}

func TestDeleteSubagentDoesNotTruncateSymlinkedTombstone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, _ := subagentRoot("parent")
	if err := os.MkdirAll(filepath.Join(root, ".deleted"), 0700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(external, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, ".deleted", "child")); err != nil {
		t.Skip(err)
	}
	if err := DeleteSubagent("parent", "child"); err == nil {
		t.Error("symlinked tombstone accepted")
	}
	if got, err := os.ReadFile(external); err != nil || string(got) != "keep" {
		t.Fatalf("external file changed: %q %v", got, err)
	}
}

func TestTaskManagementRejectsSymlinkedDirectoriesAndLock(t *testing.T) {
	for _, part := range []string{"parent", "subagents", "lock"} {
		t.Run(part, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			root, _ := subagentRoot("parent")
			if err := os.MkdirAll(dir(), 0700); err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			switch part {
			case "parent":
				if err := os.Symlink(target, filepath.Dir(root)); err != nil {
					t.Skip(err)
				}
			case "subagents":
				if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, root); err != nil {
					t.Skip(err)
				}
			case "lock":
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(target, "keep.txt")
				if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(root, ".management.lock")); err != nil {
					t.Skip(err)
				}
			}
			lock, err := lockSubagentManagement(root)
			if err == nil {
				lock.Close()
				t.Fatal("symlinked management path accepted")
			}
		})
	}
}

func TestOpenSubagentRejectsSymlinkedChildAndActiveLock(t *testing.T) {
	for _, part := range []string{"child", "active_lock"} {
		t.Run(part, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			root, _ := subagentRoot("parent")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			external := t.TempDir()
			child := filepath.Join(root, "child")
			if part == "child" {
				if err := os.Symlink(external, child); err != nil {
					t.Skip(err)
				}
			} else {
				if err := os.Mkdir(child, 0700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(external, "keep.lock")
				if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(child, ".active.lock")); err != nil {
					t.Skip(err)
				}
			}
			s, err := OpenSubagent("parent", "child", t.TempDir(), false)
			if err == nil {
				s.Close()
				t.Fatal("symlinked child execution path accepted")
			}
			if _, err := os.Stat(filepath.Join(external, ".active.lock")); !os.IsNotExist(err) {
				t.Fatalf("external execution lock created: %v", err)
			}
		})
	}
}
