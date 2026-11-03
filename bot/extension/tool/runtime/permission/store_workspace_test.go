package permission

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceRootsDoNotFollowReplacedDirectory(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	approved := filepath.Join(base, "approved")
	other := filepath.Join(base, "other")
	for _, path := range []string{approved, other} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(t.TempDir())
	if err := store.RememberWorkspaceRoot(WorkspaceRoot{Path: approved, Access: "read-write"}); err != nil {
		t.Fatal(err)
	}
	if roots := NewStore(store.root).WorkspaceRoots(); len(roots) != 1 {
		t.Fatalf("valid grant lost: %v", roots)
	}
	if err := os.Rename(approved, approved+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, approved); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if roots := NewStore(store.root).WorkspaceRoots(); len(roots) != 0 {
		t.Fatalf("replaced directory must require approval again: %v", roots)
	}
}
