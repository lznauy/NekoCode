package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/bot/extension"
)

// A FIFO holds the real skill loader in disk I/O, without adding production
// test hooks. Opening its writer acknowledges that Reload has reached the read.
func TestProjectReloadPublishesWithoutBlockingViews(t *testing.T) {
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo unavailable")
	}
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, ".nekocode", "skills", "slow", "SKILL.md")
	writeWorkspaceFile(t, path, "---\nname: old-skill\ndescription: old\n---\nold workflow")
	writeWorkspaceFile(t, filepath.Join(root, "NEKOCODE.md"), "old project instructions")
	b := newPersistTestBot(t)
	writeWorkspaceFile(t, b.project.InstructionsPath(), "new project instructions")
	writeWorkspaceFile(t, b.project.MCPPath(), `{"mcpServers":{"new-server":{"enabled":false}}}`)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(mkfifo, path).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v %s", err, out)
	}
	reading, release, writerDone, reloaded := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	go func() {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			writerDone <- err
			return
		}
		close(reading)
		<-release
		_, err = file.WriteString("---\nname: new-skill\ndescription: new\n---\nnew workflow")
		file.Close()
		writerDone <- err
	}()
	go func() { b.RefreshExtensions(); close(reloaded) }()
	defer func() {
		unblock()
		select {
		case <-reloaded:
		case <-time.After(5 * time.Second):
			t.Error("reload did not finish")
		}
		select {
		case err := <-writerDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("FIFO writer did not finish")
		}
	}()
	select {
	case <-reading:
	case <-time.After(5 * time.Second):
		t.Fatal("skill loader never reached FIFO")
	}

	view := make(chan extension.Snapshot, 1)
	go func() {
		b.CommandMenu(context.Background(), "/plugin info")
		b.Configuration() // Other Bot readers must not wait on extension I/O either.
		view <- b.Extensions()
	}()
	select {
	case snapshot := <-view:
		if len(snapshot.ConfiguredMCP) != 0 {
			t.Fatal("new configuration leaked before publication")
		}
		if !hasReloadSkill(snapshot, "old-skill") || hasReloadSkill(snapshot, "new-skill") {
			t.Fatal("view is not the previous complete snapshot")
		}
	case <-time.After(time.Second):
		t.Fatal("view blocked on extension reload")
	}
	if !strings.Contains(b.Conversation().SystemPrompt, "old project instructions") {
		t.Fatal("prompt published before extensions finished")
	}
	if menu, ok := b.CommandMenu(context.Background(), "$"); !ok || menu.Title != "Workspace refreshing" {
		t.Fatalf("expected nonblocking refresh menu: %+v, %v", menu, ok)
	}
	unblock()
	select {
	case <-reloaded:
	case <-time.After(5 * time.Second):
		t.Fatal("reload did not finish")
	}
	snapshot := b.Extensions()
	if !hasReloadSkill(snapshot, "new-skill") || hasReloadSkill(snapshot, "old-skill") || len(snapshot.ConfiguredMCP) != 1 {
		t.Fatalf("new snapshot not published: %+v", snapshot)
	}
	if !strings.Contains(b.Conversation().SystemPrompt, "new project instructions") {
		t.Fatal("new prompt not published")
	}
	if menu, ok := b.CommandMenu(context.Background(), "$"); !ok || menu.Title == "Workspace refreshing" {
		t.Fatalf("view guard not cleared after reload: %+v, %v", menu, ok)
	}
}

func hasReloadSkill(snapshot extension.Snapshot, name string) bool {
	for _, skill := range snapshot.Skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}
