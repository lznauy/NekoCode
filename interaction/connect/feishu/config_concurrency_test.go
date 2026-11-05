package feishu

import (
	"fmt"
	"nekocode/interaction/connect"
	"testing"
)

func TestStaleOwnerTouchCannotRestoreUnpairedOwner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{AppID: "a", AppSecret: "s", Owner: &connect.Owner[string]{UserID: "old", ChatID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	stale, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil).unpair(); err != nil {
		t.Fatal(err)
	}
	stale.touchOwner("old", "chat")
	if err := saveConfig(stale); err == nil {
		t.Fatal("stale owner update accepted")
	}
	current, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if current.Owner != nil {
		t.Fatal("revoked owner restored")
	}
}

func TestConcurrentConfigCommandsRejectStalePairingAndCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{}); err != nil {
		t.Fatal(err)
	}
	pair, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	// Reset must invalidate snapshots even when persisted data is unchanged.
	if err := saveConfig(Config{}); err != nil {
		t.Fatal(err)
	}
	pair.Nonce = "stale-code"
	if err := saveConfig(pair); err == nil {
		t.Fatal("stale pairing accepted after reset")
	}
	if err := saveConfig(credentials); err == nil {
		t.Fatal("stale credential command accepted after reset")
	}
}

func TestConcurrentMessagesKeepAuthorization(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{Owner: &connect.Owner[string]{UserID: "owner", ChatID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	c := New(nil)
	c.api = &feishuClient{}
	results := make(chan error, 16)
	for i := 0; i < cap(results); i++ {
		go func() {
			cfg, paired, err := c.prepareMessage(c.api, "owner", "chat", "hello")
			if err == nil && (paired || !cfg.isAllowed("owner")) {
				err = fmt.Errorf("authorized message rejected")
			}
			results <- err
		}()
	}
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err := saveConfig(Config{}); err != nil {
		t.Fatal(err)
	}
	cfg, paired, err := c.prepareMessage(c.api, "owner", "chat", "hello")
	if err != nil || paired || cfg.isAllowed("owner") {
		t.Fatalf("revoked owner authorized: paired=%v err=%v", paired, err)
	}
}
