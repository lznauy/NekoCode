package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutomaticIsolationRequiresFaithfulHead(t *testing.T) {
	root := fixture(t)
	if reason := AutomaticIsolationReason(t.Context(), root); reason != "" {
		t.Fatal(reason)
	}
	put(t, filepath.Join(root, "same.txt"), "dirty\n")
	if reason := AutomaticIsolationReason(t.Context(), root); reason != "uncommitted_changes" {
		t.Fatal(reason)
	}
	rawGit(t, root, "add", ".")
	if reason := AutomaticIsolationReason(t.Context(), root); reason != "uncommitted_changes" {
		t.Fatalf("staged changes: %s", reason)
	}
	rawGit(t, root, "commit", "-m", "changed")
	put(t, filepath.Join(root, "untracked.txt"), "new\n")
	if reason := AutomaticIsolationReason(t.Context(), root); reason != "uncommitted_changes" {
		t.Fatalf("untracked changes: %s", reason)
	}
	if reason := AutomaticIsolationReason(t.Context(), t.TempDir()); reason != "repository_unavailable" {
		t.Fatal(reason)
	}
}

func TestAutomaticIsolationDetectsHiddenChangesAndSubmodules(t *testing.T) {
	root := fixture(t)
	rawGit(t, root, "update-index", "--assume-unchanged", "same.txt")
	put(t, filepath.Join(root, "same.txt"), "hidden changes\n")
	if reason := AutomaticIsolationReason(t.Context(), root); reason != "index_flags" {
		t.Fatalf("hidden changes: %s", reason)
	}
	rawGit(t, root, "update-index", "--no-assume-unchanged", "same.txt")
	rawGit(t, root, "checkout", "--", "same.txt")
	rawGit(t, root, "update-index", "--skip-worktree", "same.txt")
	if reason := AutomaticIsolationReason(t.Context(), root); reason != "index_flags" {
		t.Fatalf("sparse index: %s", reason)
	}
	rawGit(t, root, "update-index", "--no-skip-worktree", "same.txt")
	commit := rawGit(t, root, "rev-parse", "HEAD")
	rawGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+commit+",module")
	rawGit(t, root, "commit", "-m", "gitlink")
	if reason := AutomaticIsolationReason(t.Context(), root); reason != "submodules" {
		t.Fatalf("submodule was not detected: %s", reason)
	}
}

func TestAutomaticIsolationRequiresWorkingDirectoryInHead(t *testing.T) {
	root := fixture(t)
	put(t, filepath.Join(root, ".gitignore"), "build/\n")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "src", "tracked.txt"), "tracked\n")
	rawGit(t, root, "add", ".")
	rawGit(t, root, "commit", "-m", "tracked working directory")
	for _, name := range []string{"empty", "build"} {
		cwd := filepath.Join(root, name)
		if err := os.MkdirAll(cwd, 0700); err != nil {
			t.Fatal(err)
		}
		if name == "build" {
			put(t, filepath.Join(cwd, "generated.txt"), "ignored\n")
		}
		if reason := AutomaticIsolationReason(t.Context(), cwd); reason != "working_directory_untracked" {
			t.Errorf("%s: missing directory in HEAD was accepted: %q", name, reason)
		}
	}
	if reason := AutomaticIsolationReason(t.Context(), filepath.Join(root, "src")); reason != "" {
		t.Fatalf("tracked subdirectory rejected: %s", reason)
	}
}
