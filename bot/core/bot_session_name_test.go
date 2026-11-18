package core

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/command"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
)

func TestSessionMenuShowsPersistentNameAndStableID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := session.New(t.TempDir())
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "test"}}
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	bot := &Bot{sess: manager}
	if err := bot.RenameSession(snapshot.ID, "认证修复"); err != nil {
		t.Fatal(err)
	}
	parser := command.NewParser()
	bot.registerSessionCommands(parser)
	count := 0
	for _, name := range parser.Commands() {
		if name == "/session" {
			t.Fatal("duplicate /session alias is still registered")
		}
		if name == "/sessions" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("/sessions registrations = %d, want 1", count)
	}
	for _, input := range []string{"/sessions"} {
		menu, ok := parser.Menu(context.Background(), input)
		if !ok || len(menu.Items) != 1 {
			t.Fatalf("missing menu for %s: %+v", input, menu)
		}
		item := menu.Items[0]
		if item.Label != snapshot.ID || item.Tag != "认证修复" || item.Key != snapshot.ID || item.Value != "/sessions "+snapshot.ID {
			t.Fatalf("invalid menu item: %+v", item)
		}
		output, handled := parser.Execute(context.Background(), parser.Parse(input))
		if !handled || !strings.Contains(output, "认证修复") || !strings.Contains(output, snapshot.ID) {
			t.Fatalf("invalid text list: %q", output)
		}
	}
	if manager.CurrentID() != snapshot.ID {
		t.Fatal("rename changed active session")
	}
}
