package permission

import (
	"encoding/json"
	"fmt"
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

func TestLegacyDirectoryGrantsAndTempRootReapproval(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	nested := filepath.Join(dir, "build")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir())
	// The old format persisted filepath.Dir(requestedFile).
	if err := store.mutate(func(f *permissionFile) bool {
		p := f.Projects[store.root]
		for _, file := range []string{filepath.Join(dir, "result.log"), filepath.Join(nested, "result.log")} {
			p.Workspaces = append(p.Workspaces, WorkspaceRoot{Path: filepath.Dir(file), Access: "read-write"})
		}
		f.Projects[store.root] = p
		return true
	}); err != nil {
		t.Fatal(err)
	}
	roots := store.WorkspaceRoots()
	if len(roots) != 1 || roots[0].Path != nested || (roots[0].FileOnly != nil && *roots[0].FileOnly) {
		t.Fatalf("legacy directory restoration: %+v", roots)
	}
	if err := store.RememberWorkspaceRoot(WorkspaceRoot{Path: dir, Access: "read-only"}); err != nil {
		t.Fatal(err)
	}
	roots = NewStore(store.root).WorkspaceRoots()
	if len(roots) != 2 || roots[0].Path != dir || roots[0].Access != "read-only" || roots[0].FileOnly == nil || *roots[0].FileOnly {
		t.Fatalf("explicit replacement: %+v", roots)
	}
}

func TestRememberedFileCanBeRecreatedAfterRestart(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(base, "output")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "result.log")
	if err := os.WriteFile(path, []byte("result"), 0600); err != nil {
		t.Fatal(err)
	}
	fileOnly := true
	store := NewStore(t.TempDir())
	if err := store.RememberWorkspaceRoot(WorkspaceRoot{Path: path, Access: "read-write", FileOnly: &fileOnly}); err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		roots := NewStore(store.root).WorkspaceRoots()
		if want && len(roots) == 1 && roots[0].Path == path && roots[0].Access == "read-write" && roots[0].FileOnly != nil && *roots[0].FileOnly {
			return
		}
		if want || len(roots) != 0 {
			t.Fatalf("restore file grant: want present=%t, got %+v", want, roots)
		}
	}
	check(true)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "missing"), path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	check(false)
	if err := os.Rename(parent, parent+".old"); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Symlink(base, parent); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestRememberedFileAccessSurvivesDowngradeWithoutBecomingDirectory(t *testing.T) {
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(project)
	path := filepath.Join(project, "report.log")
	// A development build wrote its scope only in the optional file_only field.
	data := fmt.Sprintf(`{"version":1,"projects":{%q:{"workspaces":[{"path":%q,"access":"read-only","file_only":true}]}}}`, project, path)
	if err := os.MkdirAll(filepath.Dir(store.Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	for _, access := range []string{"read-only", "read-write"} {
		roots := NewStore(project).WorkspaceRoots()
		if len(roots) != 1 || roots[0].FileOnly == nil || !*roots[0].FileOnly || roots[0].Path != path {
			t.Fatalf("lost exact grant before %s: %+v", access, roots)
		}
		if roots[0].Access != "read-only" {
			t.Fatalf("unexpected initial access: %+v", roots)
		}
		if access == "read-write" {
			root := roots[0]
			root.Access = access
			if err := store.RememberWorkspaceRoot(root); err != nil {
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(store.Path())
		if err != nil {
			t.Fatal(err)
		}
		// The released reader only recognizes Path and Access and skips
		// records whose access isn't read-only or read-write.
		var legacy struct {
			Projects map[string]struct {
				Workspaces []struct{ Path, Access string }
			}
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			t.Fatal(err)
		}
		stored := legacy.Projects[project].Workspaces
		if len(stored) != 1 || stored[0].Access == "read-only" || stored[0].Access == "read-write" {
			t.Fatalf("downgrade would expose a directory grant: %+v", stored)
		}
		// Rewriting through the old schema drops file_only entirely.
		raw, err = json.Marshal(legacy)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.Path(), raw, 0600); err != nil {
			t.Fatal(err)
		}
		roots = NewStore(project).WorkspaceRoots()
		if len(roots) != 1 || roots[0].Access != access || roots[0].FileOnly == nil || !*roots[0].FileOnly {
			t.Fatalf("lost %s file grant after older writer: %+v", access, roots)
		}
	}
}
