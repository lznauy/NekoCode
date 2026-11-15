package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIdleScratchHandlesAreBoundedAndReopenedByIdentity(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	r := newTaskRegistry()
	t.Cleanup(func() { _ = r.shutdown() })
	dir := t.TempDir()
	for i := 0; i <= maxTasks; i++ {
		req := startRequest{owner: "parent", actor: fmt.Sprintf("child-%d", i), directory: dir}
		if err := r.prepareScratch(&req); err != nil {
			t.Fatal(err)
		}
	}
	open := 0
	var evicted scratchIdentity
	for key, state := range r.scratch {
		if state.file != nil {
			open++
		} else {
			evicted = key
		}
	}
	if open > maxTasks || evicted.actor == "" {
		t.Fatalf("idle actor handles grew without limit: %d", open)
	}
	req := startRequest{owner: evicted.owner, actor: evicted.actor, directory: dir}
	state := r.scratch[evicted]
	if err := r.prepareScratch(&req); err != nil {
		t.Fatal(err)
	}
	info, err := req.profile.ScratchFile.Stat()
	if err != nil || !os.SameFile(state.info, info) {
		t.Fatalf("evicted handle reopened a different directory: %v", err)
	}
}

func TestScratchCleanupDoesNotDeleteReplacementAndClosesHandle(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	r := newTaskRegistry()
	t.Cleanup(func() { _ = r.shutdown() })
	req := startRequest{owner: "parent", directory: t.TempDir()}
	if err := r.prepareScratch(&req); err != nil {
		t.Fatal(err)
	}
	path, file := req.profile.ScratchDir, req.profile.ScratchFile
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(path, "unrelated-data")
	if err := os.WriteFile(canary, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.shutdown(); err == nil {
		t.Fatal("replacement was not reported")
	}
	if data, err := os.ReadFile(canary); err != nil || string(data) != "retain" {
		t.Fatalf("replacement deleted: %q %v", data, err)
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("scratch handle not closed: %v", err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if err := r.shutdown(); err != nil {
		t.Fatalf("cleanup retry failed: %v", err)
	}
}

func TestIdleScratchEvictionDoesNotCloseRunningCommandHandle(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	r := newTaskRegistry()
	t.Cleanup(func() { delete(r.tasks, "live"); _ = r.shutdown() })
	dir := t.TempDir()
	req := startRequest{owner: "parent", actor: "live", directory: dir}
	if err := r.prepareScratch(&req); err != nil {
		t.Fatal(err)
	}
	file := req.profile.ScratchFile
	r.tasks["live"] = &shellTask{status: taskRunning, scratchFile: file}
	for i := 0; i < maxTasks+2; i++ {
		next := startRequest{owner: "parent", actor: fmt.Sprintf("idle-%d", i), directory: dir}
		if err := r.prepareScratch(&next); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("live command's scratch was evicted: %v", err)
	}
}
