package telegram

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileAllowsOnlyOwner(t *testing.T) {
	profile := BotProfile{Owner: &Device{UserID: 2, ChatID: 20}}
	if !profile.isAllowed(2) {
		t.Fatal("owner should be allowed")
	}
	if profile.isAllowed(3) {
		t.Fatal("unknown user should not be allowed")
	}
}

func TestCommandMenuListsConfiguredProfileNamesWithoutTokens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{
		ActiveProfile: "personal",
		Profiles: []BotProfile{
			{Name: "work", BotToken: "work-secret"},
			{Name: "personal", BotToken: "personal-secret"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	conn := New(nil)
	for _, action := range []string{"use", "pair", "unpair", "remove"} {
		menu, ok := conn.CommandMenu(context.Background(), []string{action})
		if !ok || len(menu.Items) != 2 {
			t.Fatalf("%s menu = %+v, ok=%v", action, menu, ok)
		}
		for i, name := range []string{"personal", "work"} {
			item := menu.Items[i]
			if item.Value != "/connect telegram "+action+" "+name || !item.Submit {
				t.Errorf("%s item %d = %+v", action, i, item)
			}
			if strings.Contains(item.Value, "secret") {
				t.Fatalf("menu leaked a token: %+v", item)
			}
		}
	}
}

func TestHandleCommandTypoDoesNotRunDefaultConnect(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{
		ActiveProfile: "default",
		Profiles: []BotProfile{{
			Name: "default", BotToken: "123:secret",
			Owner: &Device{UserID: 1, ChatID: 1},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	conn := New(nil)
	got, err := conn.HandleCommand(context.Background(), []string{"conect"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Unknown Telegram command") || !strings.Contains(got, "Usage:") {
		t.Fatalf("typo response = %q, want unknown command and usage", got)
	}
	if conn.base.IsRunning() {
		t.Fatal("typo started the default connection")
	}
}

func TestHandleCommandExplicitConnectMatchesEmptyArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	conn := New(nil)
	empty, err := conn.HandleCommand(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := conn.HandleCommand(context.Background(), []string{"connect"})
	if err != nil {
		t.Fatal(err)
	}
	if explicit != empty {
		t.Fatalf("explicit connect = %q, empty args = %q", explicit, empty)
	}
}

func TestLoadConfigMigratesLegacySingleBot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".nekocode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{
  "telegram": {
    "bot_token": "123:secret",
    "bot_username": "old_bot",
    "devices": [{"user_id": 9, "username": "alice", "chat_id": 90}],
    "update_offset": 42
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "connect.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "default" || len(cfg.Profiles) != 1 {
		t.Fatalf("profiles not migrated: %#v", cfg)
	}
	p := cfg.Profiles[0]
	if p.BotToken != "123:secret" || p.BotUsername != "old_bot" || p.UpdateOffset != 42 {
		t.Fatalf("profile fields not migrated: %#v", p)
	}
	if p.Owner == nil || p.Owner.UserID != 9 || p.Owner.ChatID != 90 {
		t.Fatalf("owner not migrated: %#v", p.Owner)
	}
}
