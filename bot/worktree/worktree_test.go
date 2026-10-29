package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	root := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Fixture"}} {
		rawGit(t, root, args...)
	}
	put(t, filepath.Join(root, "same.txt"), "base\n")
	rawGit(t, root, "add", ".")
	rawGit(t, root, "commit", "-m", "base")
	return root
}
func rawGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func openTask(t *testing.T, ctx context.Context, root, id string) *Lease {
	t.Helper()
	l, e := Acquire("parent", id)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	if _, e = l.Open(ctx, root, id, false); e != nil {
		t.Fatal(e)
	}
	return l
}
func TestIsolatedConcurrentEditsResumeAndConflict(t *testing.T) {
	root := fixture(t)
	ctx := t.Context()
	put(t, filepath.Join(root, "same.txt"), "parent dirty\n")
	put(t, filepath.Join(root, "private.txt"), "parent untracked")
	a := openTask(t, ctx, root, "first")
	b := openTask(t, ctx, root, "second")
	if a.Info.Path == b.Info.Path {
		t.Fatal("shared worktrees")
	}
	for _, l := range []*Lease{a, b} {
		data, _ := os.ReadFile(filepath.Join(l.Info.Path, "same.txt"))
		if string(data) != "base\n" {
			t.Fatalf("copied dirty parent: %q", data)
		}
		if _, e := os.Stat(filepath.Join(l.Info.Path, "private.txt")); !os.IsNotExist(e) {
			t.Fatal("copied untracked file")
		}
	}
	var wg sync.WaitGroup
	for _, l := range []*Lease{a, b} {
		wg.Go(func() { put(t, filepath.Join(l.Info.Path, "same.txt"), l.Info.TaskID+"\n") })
	}
	wg.Wait()
	patch, e := a.Diff(ctx)
	if e != nil || !strings.Contains(string(patch), "+first") {
		t.Fatalf("diff: %s %v", patch, e)
	}
	if _, e = a.Apply(ctx); e == nil {
		t.Fatal("overwrote conflicting parent")
	}
	data, _ := os.ReadFile(filepath.Join(root, "same.txt"))
	if string(data) != "parent dirty\n" {
		t.Fatal("parent changed on conflict")
	}
	if _, e = Acquire("parent", "first"); e == nil {
		t.Fatal("allowed active task")
	}
	path := a.Info.Path
	a.Close()
	resumed, e := Acquire("parent", "first")
	if e != nil {
		t.Fatal(e)
	}
	defer resumed.Close()
	info, e := resumed.Open(ctx, root, "first", true)
	if e != nil || info.Path != path {
		t.Fatalf("resume: %+v %v", info, e)
	}
	put(t, filepath.Join(root, "same.txt"), "base\n")
	put(t, filepath.Join(info.Path, "new.txt"), "new\n")
	if _, e = resumed.Apply(ctx); e != nil {
		t.Fatal(e)
	}
	for name, want := range map[string]string{"same.txt": "first\n", "new.txt": "new\n"} {
		got, _ := os.ReadFile(filepath.Join(root, name))
		if string(got) != want {
			t.Errorf("%s=%q", name, got)
		}
	}
	if e = resumed.Remove(ctx, false); e == nil {
		t.Fatal("discarded modifications")
	}
	if e = resumed.Remove(ctx, true); e != nil {
		t.Fatal(e)
	}
	if _, e = resumed.Open(ctx, root, "first", true); e == nil {
		t.Fatal("resurrected removed worktree")
	}
}
func TestSafeManagementAndTemporaryIndex(t *testing.T) {
	root := fixture(t)
	ctx := t.Context()
	marker := filepath.Join(t.TempDir(), "executed")
	hooks := filepath.Join(root, ".git", "hooks")
	put(t, filepath.Join(hooks, "post-checkout"), "#!/bin/sh\ntouch '"+marker+"'\n")
	os.Chmod(filepath.Join(hooks, "post-checkout"), 0700)
	rawGit(t, root, "config", "filter.evil.smudge", "touch '"+marker+"'")
	rawGit(t, root, "config", "filter.evil.clean", "touch '"+marker+"'")
	rawGit(t, root, "config", "filter.evil.required", "true")
	put(t, filepath.Join(root, ".gitattributes"), "same.txt filter=evil\n")
	// Add attributes with filters disabled to establish the test fixture safely.
	rawGit(t, root, "-c", "filter.evil.clean=", "-c", "filter.evil.required=false", "add", ".gitattributes")
	rawGit(t, root, "-c", "core.hooksPath="+os.DevNull, "-c", "filter.evil.clean=", "-c", "filter.evil.required=false", "commit", "-m", "attributes")
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("fixture executed filter")
	}
	t.Setenv("GIT_DIR", "/missing/repository")
	l := openTask(t, ctx, root, "safe")
	put(t, filepath.Join(l.Info.Path, "same.txt"), "changed\n")
	put(t, filepath.Join(l.Info.Path, "new.txt"), "new file\n")
	before, e := output(ctx, l.Info.Path, "diff", "--cached", "--name-only")
	if e != nil {
		t.Fatal(e)
	}
	patch, e := l.Diff(ctx)
	if e != nil || !strings.Contains(string(patch), "new file") {
		t.Fatalf("diff: %s %v", patch, e)
	}
	after, e := output(ctx, l.Info.Path, "diff", "--cached", "--name-only")
	if e != nil || after != before {
		t.Fatalf("staging state changed %q -> %q: %v", before, after, e)
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("management executed repository hook or filter")
	}
}
func TestMissingRepositoryAndMissingWorktree(t *testing.T) {
	root := fixture(t)
	l, e := Acquire("parent", "missing")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if _, e = l.Open(t.Context(), t.TempDir(), "missing", false); e == nil {
		t.Fatal("accepted nonrepository")
	}
	if _, e = l.Open(t.Context(), root, "missing", true); e == nil {
		t.Fatal("recreated missing task")
	}
	if _, e = Acquire("../escape", "x"); e == nil {
		t.Fatal("accepted path traversal")
	}
}
func TestDiffIncludesCommitsAndDeletedFiles(t *testing.T) {
	root := fixture(t)
	l := openTask(t, t.Context(), root, "commit")
	rawGit(t, l.Info.Path, "rm", "same.txt")
	rawGit(t, l.Info.Path, "commit", "-m", "delete")
	patch, e := l.Diff(t.Context())
	if e != nil || !strings.Contains(string(patch), "deleted file mode") {
		t.Fatalf("lost committed diff: %s %v", patch, e)
	}
	if _, e = l.Apply(t.Context()); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "same.txt")); !os.IsNotExist(e) {
		t.Fatal("deletion was not applied")
	}
}

func TestConditionalWorktreeFiltersNeverExecute(t *testing.T) {
	root := fixture(t)
	marker := filepath.Join(t.TempDir(), "executed")
	config := filepath.Join(t.TempDir(), "filters")
	put(t, config, "[filter \"conditional\"]\n smudge = touch '"+marker+"'; cat\n clean = touch '"+marker+"'; cat\n required = true\n")
	put(t, filepath.Join(root, ".gitattributes"), "same.txt filter=conditional\n")
	rawGit(t, root, "add", ".gitattributes")
	rawGit(t, root, "commit", "-m", "attributes")
	rawGit(t, root, "config", "includeIf.gitdir:**/worktrees/**.path", config)
	l := openTask(t, t.Context(), root, "conditional")
	put(t, filepath.Join(l.Info.Path, "same.txt"), "changed\n")
	if _, e := l.Diff(t.Context()); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("conditional worktree filter executed")
	}
}

func TestInterruptedCreationCanBeDiscarded(t *testing.T) {
	root := fixture(t)
	// This directory exists only in the parent's uncommitted working tree.
	cwd := filepath.Join(root, "uncommitted-dir")
	if e := os.Mkdir(cwd, 0700); e != nil {
		t.Fatal(e)
	}
	l, e := Acquire("parent", "orphan")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if _, e = l.Open(t.Context(), cwd, "orphan", false); e == nil {
		t.Fatal("expected missing cwd in HEAD")
	}
	if _, e = l.Load(t.Context(), cwd, "orphan"); e == nil {
		t.Fatal("resumed partial creation")
	}
	if _, e = l.LoadForRemoval(t.Context(), cwd, "orphan"); e != nil {
		t.Fatal(e)
	}
	if e = l.Remove(t.Context(), true); e != nil {
		t.Fatal(e)
	}
	listed, e := List("parent")
	if e != nil || len(listed) != 0 {
		t.Fatalf("orphan not removed: %+v %v", listed, e)
	}
	// Also cover a crash between manifest persistence and git worktree add.
	p, e := Acquire("parent", "precreation")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	p.Info = l.Info
	p.Info.TaskID = "precreation"
	p.Info.Path = filepath.Join(p.dir, "tree")
	p.Info.Removed = false
	if e = p.save(); e != nil {
		t.Fatal(e)
	}
	if _, e = p.LoadForRemoval(t.Context(), root, "precreation"); e != nil {
		t.Fatal(e)
	}
	if e = p.Remove(t.Context(), true); e != nil {
		t.Fatal(e)
	}
}
func TestParallelApplyPreservesBothChanges(t *testing.T) {
	root := fixture(t)
	base := strings.Repeat("unchanged line\n", 10000)
	put(t, filepath.Join(root, "same.txt"), base)
	rawGit(t, root, "add", ".")
	rawGit(t, root, "commit", "-m", "large file")
	a := openTask(t, t.Context(), root, "apply-first")
	b := openTask(t, t.Context(), root, "apply-second")
	put(t, filepath.Join(a.Info.Path, "same.txt"), "first\n"+strings.TrimPrefix(base, "unchanged line\n"))
	put(t, filepath.Join(b.Info.Path, "same.txt"), strings.TrimSuffix(base, "unchanged line\n")+"second\n")
	var wg sync.WaitGroup
	for _, l := range []*Lease{a, b} {
		wg.Go(func() {
			if _, e := l.Apply(t.Context()); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	data, e := os.ReadFile(filepath.Join(root, "same.txt"))
	if e != nil || !strings.HasPrefix(string(data), "first\n") || !strings.HasSuffix(string(data), "second\n") {
		t.Fatal("parallel apply lost changes", e)
	}
}
func TestRemoveRetainsIgnoredArtifactsByDefault(t *testing.T) {
	root := fixture(t)
	put(t, filepath.Join(root, ".gitignore"), "artifact\n")
	rawGit(t, root, "add", ".")
	rawGit(t, root, "commit", "-m", "ignore")
	l := openTask(t, t.Context(), root, "ignored")
	put(t, filepath.Join(l.Info.Path, "artifact"), "build output")
	if e := l.Remove(t.Context(), false); e == nil {
		t.Fatal("discarded ignored file without opt-in")
	}
	if e := l.Remove(t.Context(), true); e != nil {
		t.Fatal(e)
	}
}

func TestDiffPreservesNewIgnoredTrackedFiles(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed=%v", committed), func(t *testing.T) {
			root := fixture(t)
			put(t, filepath.Join(root, ".gitignore"), "result.txt\nartifact\n")
			rawGit(t, root, "add", ".")
			rawGit(t, root, "commit", "-m", "ignore")
			l := openTask(t, t.Context(), root, "tracked-ignored")
			put(t, filepath.Join(l.Info.Path, "result.txt"), "task result\n")
			put(t, filepath.Join(l.Info.Path, "artifact"), "ignored build output\n")
			rawGit(t, l.Info.Path, "add", "-f", "result.txt")
			if committed {
				rawGit(t, l.Info.Path, "commit", "-m", "result")
			}
			before := rawGit(t, l.Info.Path, "ls-files", "--stage")
			patch, e := l.Diff(t.Context())
			if e != nil || !strings.Contains(string(patch), "+task result") {
				t.Fatalf("lost tracked ignored result: %s %v", patch, e)
			}
			if strings.Contains(string(patch), "ignored build output") {
				t.Fatal("included untracked ignored build output")
			}
			if after := rawGit(t, l.Info.Path, "ls-files", "--stage"); after != before {
				t.Fatal("changed real staging state")
			}
			if _, e = l.Apply(t.Context()); e != nil {
				t.Fatal(e)
			}
			if data, e := os.ReadFile(filepath.Join(root, "result.txt")); e != nil || string(data) != "task result\n" {
				t.Fatalf("result not applied: %q %v", data, e)
			}
			// Remove unrelated ignored output so the result itself must protect cleanup.
			if e = os.Remove(filepath.Join(l.Info.Path, "artifact")); e != nil {
				t.Fatal(e)
			}
			if e = l.Remove(t.Context(), false); e == nil {
				t.Fatal("default cleanup discarded task result")
			}
		})
	}
}

type recordingApplyCheckpoint struct {
	begin     int
	captured  []string
	finalized []string
	capture   func(string) error
}

func (c *recordingApplyCheckpoint) Begin() error { c.begin++; return nil }
func (c *recordingApplyCheckpoint) Capture(path string) error {
	if c.capture != nil {
		if err := c.capture(path); err != nil {
			return err
		}
	}
	c.captured = append(c.captured, path)
	return nil
}
func (c *recordingApplyCheckpoint) Finalize(path string) error {
	c.finalized = append(c.finalized, path)
	return nil
}

func TestApplyCheckpointPathsAndFileChanges(t *testing.T) {
	root := fixture(t)
	put(t, filepath.Join(root, "delete.txt"), "delete\n")
	put(t, filepath.Join(root, "mode.sh"), "#!/bin/sh\n")
	put(t, filepath.Join(root, "old name.txt"), "rename me\n")
	rawGit(t, root, "add", ".")
	rawGit(t, root, "commit", "-m", "apply fixtures")
	l := openTask(t, t.Context(), root, "checkpoint-paths")
	put(t, filepath.Join(l.Info.Path, "same.txt"), "modified\n")
	if err := os.Remove(filepath.Join(l.Info.Path, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(l.Info.Path, "mode.sh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(l.Info.Path, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join("dir", "new \t\n-name.txt")
	if err := os.Rename(filepath.Join(l.Info.Path, "old name.txt"), filepath.Join(l.Info.Path, renamed)); err != nil {
		t.Fatal(err)
	}
	created := "new \t\n-file.bin"
	binary := []byte{0, 1, 2, 0, 255}
	if err := os.WriteFile(filepath.Join(l.Info.Path, created), binary, 0600); err != nil {
		t.Fatal(err)
	}
	cp := new(recordingApplyCheckpoint)
	changed, err := l.Apply(t.Context(), cp)
	if err != nil || !changed {
		t.Fatalf("apply changed=%v err=%v", changed, err)
	}
	if cp.begin != 1 {
		t.Fatalf("Begin calls = %d", cp.begin)
	}
	expected := map[string]bool{}
	for _, name := range []string{"same.txt", "delete.txt", "mode.sh", "old name.txt", renamed, created} {
		expected[filepath.Join(root, name)] = true
	}
	if len(cp.captured) != len(expected) {
		t.Fatalf("captured paths = %q, want %d", cp.captured, len(expected))
	}
	for _, path := range cp.captured {
		if !expected[path] {
			t.Errorf("unexpected captured path %q", path)
		}
		delete(expected, path)
	}
	for path := range expected {
		t.Errorf("missing captured path %q", path)
	}
	if len(cp.finalized) != len(cp.captured) {
		t.Fatalf("finalized = %q, captured = %q", cp.finalized, cp.captured)
	}
	for i := range cp.captured {
		if cp.finalized[i] != cp.captured[i] {
			t.Errorf("Finalize[%d] = %q, want %q", i, cp.finalized[i], cp.captured[i])
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "same.txt")); err != nil || string(data) != "modified\n" {
		t.Fatalf("modified file = %q, %v", data, err)
	}
	for _, name := range []string{"delete.txt", "old name.txt"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", name, err)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "mode.sh")); err != nil || info.Mode().Perm()&0100 == 0 {
		t.Errorf("executable mode not applied: %v %v", info, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, renamed)); err != nil || string(data) != "rename me\n" {
		t.Errorf("renamed file = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, created)); err != nil || !bytes.Equal(data, binary) {
		t.Errorf("created binary = %v, %v", data, err)
	}
}

func TestApplyCheckpointSkippedForEmptyConflictAndUnsupportedType(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		root := fixture(t)
		l := openTask(t, t.Context(), root, "empty")
		cp := new(recordingApplyCheckpoint)
		changed, err := l.Apply(t.Context(), cp)
		if err != nil || changed || cp.begin != 0 {
			t.Fatalf("changed=%v begin=%d err=%v", changed, cp.begin, err)
		}
	})
	t.Run("conflict", func(t *testing.T) {
		root := fixture(t)
		l := openTask(t, t.Context(), root, "conflict")
		put(t, filepath.Join(l.Info.Path, "same.txt"), "task\n")
		put(t, filepath.Join(root, "same.txt"), "parent\n")
		cp := new(recordingApplyCheckpoint)
		changed, err := l.Apply(t.Context(), cp)
		if err == nil || changed || cp.begin != 0 {
			t.Fatalf("changed=%v begin=%d err=%v", changed, cp.begin, err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root := fixture(t)
		l := openTask(t, t.Context(), root, "symlink")
		if err := os.Symlink("same.txt", filepath.Join(l.Info.Path, "link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		cp := new(recordingApplyCheckpoint)
		changed, err := l.Apply(t.Context(), cp)
		if err == nil || changed || cp.begin != 0 || !strings.Contains(err.Error(), "checkpoint does not support") {
			t.Fatalf("changed=%v begin=%d err=%v", changed, cp.begin, err)
		}
		if _, err := os.Lstat(filepath.Join(root, "link")); !os.IsNotExist(err) {
			t.Fatalf("symlink was applied: %v", err)
		}
		changed, err = l.Apply(t.Context())
		if err != nil || !changed {
			t.Fatalf("apply without checkpoint changed=%v err=%v", changed, err)
		}
		if target, err := os.Readlink(filepath.Join(root, "link")); err != nil || target != "same.txt" {
			t.Fatalf("symlink without checkpoint = %q, %v", target, err)
		}
	})
}

func TestCheckpointPathsRejectGitlink(t *testing.T) {
	raw := []byte(":160000 160000 1111111 2222222 M\x00submodule\x00")
	if _, err := checkpointPaths(t.TempDir(), raw); err == nil || !strings.Contains(err.Error(), "checkpoint does not support") {
		t.Fatalf("gitlink plan err=%v", err)
	}
}

func TestApplyCheckpointCaptureFailurePreventsWrites(t *testing.T) {
	root := fixture(t)
	l := openTask(t, t.Context(), root, "capture-failure")
	put(t, filepath.Join(l.Info.Path, "same.txt"), "modified\n")
	put(t, filepath.Join(l.Info.Path, "new.txt"), "new\n")
	cp := &recordingApplyCheckpoint{}
	cp.capture = func(string) error {
		if len(cp.captured) == 1 {
			return errors.New("capture failed")
		}
		return nil
	}
	changed, err := l.Apply(t.Context(), cp)
	if err == nil || changed || !strings.Contains(err.Error(), "capture failed") {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if data, readErr := os.ReadFile(filepath.Join(root, "same.txt")); readErr != nil || string(data) != "base\n" {
		t.Fatalf("file written after Capture failure: %q %v", data, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("new file written after Capture failure: %v", statErr)
	}
	if len(cp.captured) != 1 || len(cp.finalized) != 1 || cp.finalized[0] != cp.captured[0] {
		t.Fatalf("captured=%q finalized=%q", cp.captured, cp.finalized)
	}
}

func TestApplyCheckpointFinalizesAfterApplyError(t *testing.T) {
	root := fixture(t)
	l := openTask(t, t.Context(), root, "apply-error")
	put(t, filepath.Join(l.Info.Path, "same.txt"), "modified\n")
	cp := &recordingApplyCheckpoint{}
	cp.capture = func(path string) error {
		// The preflight has already passed. Simulate an external write before
		// git's real apply to exercise its error cleanup path.
		return os.WriteFile(path, []byte("external\n"), 0600)
	}
	changed, err := l.Apply(t.Context(), cp)
	if err == nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if len(cp.captured) != 1 || len(cp.finalized) != 1 || cp.finalized[0] != cp.captured[0] {
		t.Fatalf("captured=%q finalized=%q", cp.captured, cp.finalized)
	}
	if data, readErr := os.ReadFile(filepath.Join(root, "same.txt")); readErr != nil || string(data) != "external\n" {
		t.Fatalf("unexpected parent state: %q %v", data, readErr)
	}
}
