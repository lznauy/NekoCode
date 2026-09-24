package core

import (
	"context"
	"testing"
	"time"

	"nekocode/bot/command"
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
