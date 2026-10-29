package feishu

import (
	"context"
	"strings"
	"testing"
)

func TestHandleCommandTypoDoesNotRunDefaultConnect(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{AppID: "cli_test", AppSecret: "secret"}
	cfg.finishPairing("owner", "chat")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	conn := New(nil)
	got, err := conn.HandleCommand(context.Background(), []string{"conect"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Unknown Feishu command") || !strings.Contains(got, "Usage:") {
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
	// Pairing 状态机本身(Start/Valid/过期)由 connect 根包测试覆盖,
	// 这里只测飞书侧的配对接线。
	cfg := Config{}
	cfg.finishPairing("ou_1", "oc_1")
	if cfg.Pairing.Nonce != "" || cfg.Pairing.Expires != 0 {
		t.Fatal("pairing state should be cleared")
	}
	if !cfg.isAllowed("ou_1") {
		t.Fatal("owner open_id should be allowed")
	}
	if cfg.isAllowed("ou_2") {
		t.Fatal("other open_id should not be allowed")
	}
	if got := cfg.pairedChatIDs(); len(got) != 1 || got[0] != "oc_1" {
		t.Fatalf("pairedChatIDs = %v, want [oc_1]", got)
	}
}

func TestUnpairAndTouchOwner(t *testing.T) {
	cfg := Config{}
	cfg.finishPairing("ou_1", "oc_1")

	// Owner.Touch 的语义由根包覆盖,这里只验证飞书侧接线成功路径。
	cfg.touchOwner("ou_1", "oc_2")
	if cfg.Owner.ChatID != "oc_2" {
		t.Fatal("owner chat id should be updated")
	}

	cfg.unpair()
	if cfg.Owner != nil || cfg.isAllowed("ou_1") || len(cfg.pairedChatIDs()) != 0 {
		t.Fatal("unpair should clear owner and chats")
	}
}

func TestConfigured(t *testing.T) {
	if (Config{}).configured() {
		t.Fatal("empty config should not be configured")
	}
	if (Config{AppID: "  ", AppSecret: "sec"}).configured() {
		t.Fatal("whitespace-only app id should not be configured")
	}
	if !(Config{AppID: "cli_a", AppSecret: "sec"}).configured() {
		t.Fatal("app id + secret should be configured")
	}
}
