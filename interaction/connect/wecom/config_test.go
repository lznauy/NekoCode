package wecom

import (
	"context"
	"strings"
	"testing"
)

func TestHandleCommandTypoDoesNotRunDefaultConnect(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{BotID: "bot", Secret: "secret"}
	cfg.finishPairing("owner", "chat")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	conn := New(nil)
	got, err := conn.HandleCommand(context.Background(), []string{"conect"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Unknown WeCom command") || !strings.Contains(got, "Usage:") {
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

func TestConfigPairingLifecycle(t *testing.T) {
	cfg := Config{BotID: "bot", Secret: "secret"}
	if !cfg.configured() {
		t.Fatal("bot id and secret should configure connector")
	}
	cfg.finishPairing("zhangsan", "group-1")
	if !cfg.isAllowed("zhangsan") || cfg.isAllowed("lisi") {
		t.Fatal("only the paired owner should be allowed")
	}
	if cfg.Owner.ChatID != "group-1" || cfg.Pairing.Nonce != "" {
		t.Fatalf("paired config = %#v", cfg)
	}
	cfg.touchOwner("zhangsan", "zhangsan")
	if cfg.Owner.ChatID != "zhangsan" {
		t.Fatal("the latest owner conversation should receive outbound events")
	}
	cfg.unpair()
	if cfg.Owner != nil || cfg.isAllowed("zhangsan") {
		t.Fatal("unpair should clear owner access")
	}
}

func TestConfigRequiresCompleteCredentials(t *testing.T) {
	for _, cfg := range []Config{{}, {BotID: "bot"}, {Secret: "secret"}, {BotID: " ", Secret: "secret"}} {
		if cfg.configured() {
			t.Fatalf("incomplete config reported configured: %#v", cfg)
		}
	}
}
