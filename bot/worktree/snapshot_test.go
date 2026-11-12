package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestDirtyWorkspaceSnapshotsPreserveReviewAndApplyOnlyTaskDelta(t *testing.T) {
	root := fixture(t)
	put(t, filepath.Join(root, ".gitignore"), "ignored.txt\n")
	put(t, filepath.Join(root, "gone.txt"), "delete me\n")
	rawGit(t, root, "add", ".")
	rawGit(t, root, "commit", "-m", "baseline")
	head := rawGit(t, root, "rev-parse", "HEAD")
	put(t, filepath.Join(root, "same.txt"), "staged\n")
	rawGit(t, root, "add", "same.txt")
	put(t, filepath.Join(root, "same.txt"), "unstaged\n")
	put(t, filepath.Join(root, "new.txt"), "parent untracked\n")
	put(t, filepath.Join(root, "ignored.txt"), "excluded\n")
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	status := rawGit(t, root, "status", "--porcelain=v1", "--untracked-files=all")
	staged := rawGit(t, root, "diff", "--cached", "--no-ext-diff")
	unstaged := rawGit(t, root, "diff", "--no-ext-diff")
	leases := make([]*Lease, 2)
	for i, id := range []string{"snap-a", "snap-b"} {
		l, err := Acquire("parent", id)
		if err != nil {
			t.Fatal(err)
		}
		leases[i] = l
		t.Cleanup(func() { l.Close() })
	}
	var wg sync.WaitGroup
	for _, l := range leases {
		wg.Go(func() {
			if _, err := l.OpenSnapshot(t.Context(), root, filepath.Base(l.dir)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if t.Failed() {
		return
	}
	for _, l := range leases {
		if !l.Info.Snapshot || l.Info.SourceHead != head {
			t.Fatalf("snapshot provenance missing: %+v", l.Info)
		}
		if got := rawGit(t, l.Info.Path, "status", "--porcelain=v1", "--untracked-files=all"); got != status {
			t.Fatalf("review status changed: %q want %q", got, status)
		}
		if got := rawGit(t, l.Info.Path, "diff", "--cached", "--no-ext-diff"); got != staged {
			t.Fatalf("staged diff changed: %q", got)
		}
		if got := rawGit(t, l.Info.Path, "diff", "--no-ext-diff"); got != unstaged {
			t.Fatalf("unstaged diff changed: %q", got)
		}
		if _, err := os.Stat(filepath.Join(l.Info.Path, "ignored.txt")); !os.IsNotExist(err) {
			t.Fatal("copied ignored file")
		}
		if patch, err := l.Diff(t.Context()); err != nil || len(patch) != 0 {
			t.Fatalf("initial copied changes leaked into integration: %s %v", patch, err)
		}
		if changed, err := l.Apply(t.Context()); err != nil || changed {
			t.Fatalf("empty task delta altered parent: %v %v", changed, err)
		}
	}
	if got := rawGit(t, root, "status", "--porcelain=v1", "--untracked-files=all"); got != status {
		t.Fatalf("snapshot changed parent: %q", got)
	}
	if got := rawGit(t, root, "diff", "--cached", "--no-ext-diff"); got != staged {
		t.Fatal("snapshot changed parent index")
	}
	a, b := leases[0], leases[1]
	put(t, filepath.Join(a.Info.Path, "same.txt"), "worker result\n")
	patch, err := a.Diff(t.Context())
	if err != nil || !strings.Contains(string(patch), "-unstaged") || strings.Contains(string(patch), "new.txt") || strings.Contains(string(patch), "gone.txt") {
		t.Fatalf("incorrect task-only delta: %s %v", patch, err)
	}
	if changed, err := a.Apply(t.Context()); err != nil || !changed {
		t.Fatalf("apply: %v %v", changed, err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "same.txt")); string(got) != "worker result\n" {
		t.Fatalf("apply: %s", got)
	}
	if got := rawGit(t, root, "diff", "--cached", "--no-ext-diff"); got != staged {
		t.Fatal("apply changed original staged content")
	}
	if got, _ := os.ReadFile(filepath.Join(b.Info.Path, "same.txt")); string(got) != "unstaged\n" {
		t.Fatal("snapshot copies interfered")
	}
	put(t, filepath.Join(b.Info.Path, "same.txt"), "conflicting worker\n")
	if _, err := b.Apply(t.Context()); err == nil {
		t.Fatal("conflicting task overwrote integrated result")
	}
	a.Close()
	resumed, err := Acquire("parent", "snap-a")
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	info, err := resumed.Open(t.Context(), root, "snap-a", true)
	if err != nil || !info.Snapshot || info.Path != a.Info.Path || info.Base != a.Info.Base {
		t.Fatalf("snapshot resume changed base: %+v %v", info, err)
	}
	if err := resumed.Remove(t.Context(), true); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotPreservesIntentToAddAndSplitIndex(t *testing.T) {
	root := fixture(t)
	put(t, filepath.Join(root, "pending.txt"), "intent to add\n")
	rawGit(t, root, "add", "-N", "pending.txt")
	rawGit(t, root, "update-index", "--split-index")
	status := rawGit(t, root, "status", "--porcelain=v1")
	staged := rawGit(t, root, "diff", "--cached")
	l, err := Acquire("parent", "intent")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	info, err := l.OpenSnapshot(t.Context(), root, "intent")
	if err != nil {
		t.Fatal(err)
	}
	if got := rawGit(t, info.Path, "status", "--porcelain=v1"); got != status {
		t.Fatalf("intent-to-add changed: %q want %q", got, status)
	}
	if got := rawGit(t, info.Path, "diff", "--cached"); got != staged {
		t.Fatalf("intent-to-add became staged: %q", got)
	}
	if patch, err := l.Diff(t.Context()); err != nil || len(patch) != 0 {
		t.Fatalf("intent-to-add leaked into task delta: %s %v", patch, err)
	}
}

func TestSnapshotBasesArePrivateAndSurviveGC(t *testing.T) {
	root := fixture(t)
	var leases []*Lease
	for _, id := range []string{"first-copy", "second-copy"} {
		put(t, filepath.Join(root, "same.txt"), id+"\n")
		l, err := Acquire("parent", id)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		if _, err := l.OpenSnapshot(t.Context(), root, id); err != nil {
			t.Fatal(err)
		}
		leases = append(leases, l)
	}
	if leases[0].Info.Base == leases[1].Info.Base {
		t.Fatal("different snapshots share baseline")
	}
	rawGit(t, root, "gc", "--prune=now")
	for _, l := range leases {
		if got := rawGit(t, l.Info.Path, "rev-parse", l.snapshotBaseRef()); got != l.Info.Base {
			t.Fatalf("snapshot pin changed: %s", got)
		}
		id := filepath.Base(l.dir)
		l.Close()
		resumed, err := Acquire("parent", id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := resumed.Open(t.Context(), root, id, true); err != nil {
			resumed.Close()
			t.Fatal(err)
		}
		resumed.Close()
	}
}

func TestSnapshotManagementDoesNotRunHooksFiltersOrSigning(t *testing.T) {
	root := fixture(t)
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "unsafe-helper")
	put(t, script, "#!/bin/sh\ntouch '"+marker+"'\nexit 1\n")
	if err := os.Chmod(script, 0700); err != nil {
		t.Fatal(err)
	}
	rawGit(t, root, "config", "filter.test.clean", script)
	rawGit(t, root, "config", "filter.test.smudge", script)
	rawGit(t, root, "config", "filter.test.required", "true")
	rawGit(t, root, "config", "commit.gpgSign", "true")
	rawGit(t, root, "config", "gpg.program", script)
	rawGit(t, root, "config", "core.hooksPath", filepath.Dir(script))
	for _, name := range []string{"post-checkout", "reference-transaction"} {
		hook := filepath.Join(filepath.Dir(script), name)
		put(t, hook, "#!/bin/sh\ntouch '"+marker+"'\nexit 1\n")
		if err := os.Chmod(hook, 0700); err != nil {
			t.Fatal(err)
		}
	}
	put(t, filepath.Join(root, ".gitattributes"), "same.txt filter=test\n")
	put(t, filepath.Join(root, "same.txt"), "snapshot current\n")
	l, err := Acquire("parent", "safe-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err := l.OpenSnapshot(t.Context(), root, "safe-snapshot"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("snapshot management ran helper")
	}
}

func TestSnapshotRejectsUntrackedEmbeddedRepository(t *testing.T) {
	root := fixture(t)
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	rawGit(t, nested, "init")
	rawGit(t, nested, "config", "user.email", "fixture@example.invalid")
	rawGit(t, nested, "config", "user.name", "Fixture")
	put(t, filepath.Join(nested, "nested.txt"), "must not silently disappear\n")
	rawGit(t, nested, "add", ".")
	rawGit(t, nested, "commit", "-m", "nested commit")
	l, err := Acquire("parent", "nested-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err := l.OpenSnapshot(t.Context(), root, "nested-snapshot"); err == nil || !strings.Contains(err.Error(), "submodules") {
		t.Fatalf("embedded repository was silently omitted: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(nested, "nested.txt")); string(got) != "must not silently disappear\n" {
		t.Fatal("source files changed")
	}
}

func TestRemovalFailureRetainsCleanupState(t *testing.T) {
	root := fixture(t)
	l, err := Acquire("parent", "cleanup")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err = l.OpenSnapshot(t.Context(), root, "cleanup"); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(l.Info.CommonDir, filepath.FromSlash(l.snapshotBaseRef())+".lock")
	if err = os.MkdirAll(filepath.Dir(lock), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(lock, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if err = l.Remove(t.Context(), true); err == nil {
		t.Fatal("expected locked ref deletion failure")
	}
	items, err := List("parent")
	if err != nil || len(items) != 1 || !items[0].Removing || items[0].Removed {
		t.Fatalf("cleanup progress lost: %+v %v", items, err)
	}
	if _, err = l.LoadForRemoval(t.Context(), root, "cleanup"); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err = l.Remove(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	items, err = List("parent")
	if err != nil || len(items) != 0 {
		t.Fatalf("removed task still listed: %+v %v", items, err)
	}
}

func TestRemovalRetryStillProtectsNewFiles(t *testing.T) {
	root := fixture(t)
	l, err := Acquire("parent", "retry-safe")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err = l.Open(t.Context(), root, "retry-safe", false); err != nil {
		t.Fatal(err)
	}
	rawGit(t, root, "worktree", "lock", l.Info.Path)
	if err = l.Remove(t.Context(), false); err == nil || !l.Info.Removing {
		t.Fatalf("expected pending removal: %v", err)
	}
	rawGit(t, root, "worktree", "unlock", l.Info.Path)
	file := filepath.Join(l.Info.Path, "valuable-untracked")
	put(t, file, "keep me")
	if _, err = l.LoadForRemoval(t.Context(), root, "retry-safe"); err != nil {
		t.Fatal(err)
	}
	if err = l.Remove(t.Context(), false); err == nil {
		t.Fatal("retry discarded newly created files")
	}
	if content, err := os.ReadFile(file); err != nil || string(content) != "keep me" {
		t.Fatal("new file lost")
	}
}

func TestRemovalLoaderRejectsInvalidBase(t *testing.T) {
	root := fixture(t)
	l, err := Acquire("parent", "invalid-base")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err = l.Open(t.Context(), root, "invalid-base", false); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	put(t, target, "preserve")
	l.Info.Base = "--output=" + target
	if _, err = l.Diff(t.Context()); err == nil {
		t.Fatal("diff accepted a git option as base")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "preserve" {
		t.Fatal("invalid base overwrote target")
	}
	if err = l.save(); err != nil {
		t.Fatal(err)
	}
	if _, err = l.LoadForRemoval(t.Context(), root, "invalid-base"); err == nil {
		t.Fatal("removal accepted a git option as base")
	}
}

func TestFailedCreationRollsBackTreeAndSnapshotRef(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test injects a POSIX git wrapper")
	}
	for _, snapshot := range []bool{false, true} {
		t.Run(fmt.Sprint(snapshot), func(t *testing.T) {
			root := fixture(t)
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			shim := t.TempDir()
			script := `#!/bin/sh
case " $* " in
 *" worktree add "*) "$NEKO_TEST_REAL_GIT" "$@" || exit $?; exit 77 ;;
esac
exec "$NEKO_TEST_REAL_GIT" "$@"
`
			if err = os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			originalPath := os.Getenv("PATH")
			t.Setenv("NEKO_TEST_REAL_GIT", realGit)
			t.Setenv("PATH", shim+string(os.PathListSeparator)+originalPath)
			l, err := Acquire("parent", "rollback")
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			if snapshot {
				_, err = l.OpenSnapshot(t.Context(), root, "rollback")
			} else {
				_, err = l.Open(t.Context(), root, "rollback", false)
			}
			if err == nil {
				t.Fatal("injected creation failure did not fire")
			}
			for _, name := range []string{"tree", "meta.json"} {
				if _, err = os.Lstat(filepath.Join(l.dir, name)); !os.IsNotExist(err) {
					t.Fatalf("failed creation retained %s: %v", name, err)
				}
			}
			if snapshot {
				if _, err = output(t.Context(), root, "rev-parse", "--verify", l.snapshotBaseRef()); err == nil {
					t.Fatal("snapshot pin retained")
				}
			}
			t.Setenv("PATH", originalPath)
			if snapshot {
				_, err = l.OpenSnapshot(t.Context(), root, "rollback")
			} else {
				_, err = l.Open(t.Context(), root, "rollback", false)
			}
			if err != nil {
				t.Fatalf("same-ID retry failed: %v", err)
			}
		})
	}
}

func TestCreationDoesNotDiscardPreexistingUnregisteredFiles(t *testing.T) {
	root := fixture(t)
	l, err := Acquire("parent", "existing")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	path := filepath.Join(l.dir, "tree")
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "valuable")
	put(t, file, "preserve")
	if _, err = l.Open(t.Context(), root, "existing", false); err == nil {
		t.Fatal("creation accepted preexisting files")
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "preserve" {
		t.Fatal("creation deleted preexisting files")
	}
}
