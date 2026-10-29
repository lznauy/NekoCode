package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekocode/bot/command"
	"nekocode/bot/session"
	"nekocode/bot/worktree"
)

// Regression: the permission menu reads the full-takeover mode while
// Bot.CommandMenu holds b.mu. Reading it through getAgent (which takes the
// same non-reentrant mutex) deadlocked the UI on Enter. The menu must use
// the lock-free mirror instead — this test hangs on any regression.
func TestPermissionMenuDoesNotDeadlockUnderBotLock(t *testing.T) {
	b := &Bot{}
	b.cmd = command.New(command.Deps{})
	b.registerCommandMenus(b.cmd.Parser())

	done := make(chan struct{})
	go func() {
		defer close(done)
		menu, ok := b.CommandMenu(context.Background(), "/permission")
		if !ok || len(menu.Items) != 2 {
			t.Errorf("permission menu = %+v, %v", menu, ok)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CommandMenu(/permission) deadlocked")
	}
}

func TestPermissionMenuShowsAutoOnlyWhenJevIsAvailable(t *testing.T) {
	b := &Bot{}
	b.cmd = command.New(command.Deps{})
	b.registerCommandMenus(b.cmd.Parser())
	menu, ok := b.CommandMenu(context.Background(), "/permission")
	if !ok || len(menu.Items) != 2 {
		t.Fatalf("menu without Jev = %+v, %v", menu, ok)
	}
	b.jevAvailable.Store(true)
	menu, ok = b.CommandMenu(context.Background(), "/permission")
	if !ok || len(menu.Items) != 3 || menu.Items[1].Value != "/permission auto" {
		t.Fatalf("menu with Jev = %+v, %v", menu, ok)
	}
}

func TestWorktreeMenuDrivesDynamicTaskCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := &Bot{sess: session.New(t.TempDir())}
	parser := command.NewParser()
	b.registerWorktreeCommands(parser)
	b.registerCommandMenus(parser)

	root, ok := parser.Menu(t.Context(), "/worktree")
	if !ok || len(root.Items) != 4 {
		t.Fatalf("worktree root menu = %+v, %v", root, ok)
	}
	wantRoot := []string{"/worktree list", "/worktree diff", "/worktree apply", "/worktree remove"}
	for i, item := range root.Items {
		if item.Value != wantRoot[i] || item.Submit != (i == 0) {
			t.Fatalf("root item %d = %+v", i, item)
		}
	}

	for _, action := range []string{"diff", "apply", "remove"} {
		menu, ok := parser.Menu(t.Context(), "/worktree "+action)
		if !ok || len(menu.Items) != 0 || menu.Empty == "" {
			t.Fatalf("empty %s menu = %+v, %v", action, menu, ok)
		}
	}

	writeWorktreeMenuFixture(t, b.sess.CurrentID(), "first")
	writeWorktreeMenuFixture(t, b.sess.CurrentID(), "second")
	writeWorktreeMenuFixture(t, "another-session", "other")
	for _, action := range []string{"diff", "apply", "remove"} {
		menu, ok := parser.Menu(t.Context(), "/worktree "+action)
		wantItems := 2
		if action == "remove" {
			wantItems = 4
		}
		if !ok || len(menu.Items) != wantItems {
			t.Fatalf("%s task menu = %+v, %v", action, menu, ok)
		}
		for i, item := range menu.Items {
			taskID := []string{"first", "second"}[i/2]
			if action != "remove" {
				taskID = []string{"first", "second"}[i]
			}
			want := "/worktree " + action + " " + taskID
			if action == "remove" && i%2 == 1 {
				want += " --discard"
				if !strings.Contains(item.Description, "WARNING") {
					t.Fatalf("discard item lacks warning: %+v", item)
				}
			}
			if item.Value != want || !item.Submit {
				t.Fatalf("%s item %d = %+v, want value %q and submit", action, i, item, want)
			}
		}
	}

	for _, input := range []string{
		"/worktree list", "/worktree diff first", "/worktree apply custom-id",
		"/worktree remove first", "/worktree remove first --discard", "/worktree unknown",
	} {
		if menu, ok := parser.Menu(t.Context(), input); ok {
			t.Fatalf("direct input %q opened menu %+v", input, menu)
		}
	}
	output, handled := parser.Execute(t.Context(), parser.Parse("/worktree list"))
	if !handled || !strings.Contains(output, "first") || !strings.Contains(output, "second") || strings.Contains(output, "other") {
		t.Fatalf("worktree list output = %q, %v", output, handled)
	}
}

func TestWorkspaceMenuValuesAndDirectArguments(t *testing.T) {
	b := &Bot{}
	parser := command.NewParser()
	b.registerWorkspaceCommands(parser)
	b.registerCommandMenus(parser)

	menu, ok := parser.Menu(t.Context(), "/workspace")
	if !ok || len(menu.Items) != 2 {
		t.Fatalf("workspace menu = %+v, %v", menu, ok)
	}
	if item := menu.Items[0]; item.Label != "查看配置" || item.Value != "/workspace" || !item.Submit {
		t.Fatalf("workspace view item = %+v", item)
	}
	if item := menu.Items[1]; item.Label != "reload" || item.Value != "/workspace reload" || !item.Submit {
		t.Fatalf("workspace reload item = %+v", item)
	}
	output, handled := parser.Execute(t.Context(), parser.Parse(menu.Items[0].Value))
	if !handled || output != "No project configuration loaded." {
		t.Fatalf("workspace view output = %q, %v", output, handled)
	}
	for _, input := range []string{"/workspace reload", "/workspace unexpected"} {
		if menu, ok := parser.Menu(t.Context(), input); ok {
			t.Fatalf("direct input %q opened menu %+v", input, menu)
		}
	}
}

func writeWorktreeMenuFixture(t *testing.T, sessionID, taskID string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".nekocode", "worktrees", sessionID, taskID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(worktree.Info{TaskID: taskID, Path: filepath.Join(dir, "repo")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
