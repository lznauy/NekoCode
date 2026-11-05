package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseNumstat(t *testing.T) {
	added, deleted := parseNumstat([]byte("12\t3\ta.go\x00-\t-\tasset.bin\x004\t8\tb.go\x00"))
	if added != 16 || deleted != 11 {
		t.Fatalf("parseNumstat = +%d -%d, want +16 -11", added, deleted)
	}
}

func TestCountNULTerms(t *testing.T) {
	if got := countNULTerms([]byte("a\x00b c\x00")); got != 2 {
		t.Fatalf("countNULTerms = %d, want 2", got)
	}
}

func TestWorkspaceChangesDisablesFsmonitor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script probe")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
	}
	run("init", "-q")
	marker := filepath.Join(dir, "executed")
	script := filepath.Join(dir, "monitor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	run("config", "core.fsmonitor", script)
	if got := (&Bot{cwd: dir}).WorkspaceChanges(); !got.Available {
		t.Fatal("workspace changes unavailable")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("fsmonitor executed: %v", err)
	}
}
func TestWorkspaceGitIgnoresInheritedGitDirectory(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git: %v: %s", err, out)
	}
	t.Setenv("GIT_DIR", filepath.Join(other, "missing"))
	out, err := gitOutput(context.Background(), dir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(string(out)) != dir {
		t.Fatalf("wrong repository: %q %v", out, err)
	}
}

func TestWorkspaceChangesDisablesCleanFilters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script probe")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
	}
	run("init", "-q")
	file := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+os.DevNull, "commit", "-qm", "baseline")
	marker := filepath.Join(t.TempDir(), "executed")
	run("config", "filter.evil.clean", "touch '"+marker+"'; cat")
	run("config", "filter.evil.required", "true")
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("tracked.txt filter=evil\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("after and longer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := (&Bot{cwd: dir}).WorkspaceChanges()
	if !got.Available || got.Added != 1 || got.Deleted != 1 {
		t.Fatalf("unexpected changes: %+v", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("clean filter executed: %v", err)
	}
}
