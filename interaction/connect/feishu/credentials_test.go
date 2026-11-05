package feishu

import (
	"context"
	"errors"
	"testing"
	"time"

	"nekocode/interaction/connect"
)

func TestCredentialChangeStopsOldConnectionAndRejectsItsNonce(t *testing.T) {
	for _, tc := range []struct{ name, app, secret string }{{"app change", "new-app", "new-secret"}, {"secret rotation", "old-app", "new-secret"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if err := saveConfig(Config{AppID: "old-app", AppSecret: "old-secret", Owner: &connect.Owner[string]{UserID: "owner", ChatID: "chat"}}); err != nil {
				t.Fatal(err)
			}
			c := New(nil)
			runCtx, _ := c.base.Start(context.Background())
			defer c.base.Stop()
			old := newFeishuClient("old-app", "old-secret")
			c.api = old
			if _, err := c.addApp([]string{tc.app, tc.secret}); err != nil {
				t.Fatal(err)
			}
			if c.api != nil || c.base.IsRunning() || runCtx.Err() == nil {
				t.Fatal("old credential connection remains active")
			}
			cfg, err := loadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if (cfg.Owner == nil) != (tc.app != "old-app") {
				t.Fatal("owner reset does not match app identity change")
			}
			cfg.Owner = nil
			cfg.Start("fresh-nonce", time.Now(), time.Minute)
			if err := saveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			fresh := newFeishuClient(tc.app, tc.secret)
			c.api = fresh
			if _, _, err := c.prepareMessage(old, "old-user", "old-chat", "fresh-nonce"); !errors.Is(err, errConfigChanged) {
				t.Fatalf("old client accepted: %v", err)
			}
			saved, err := loadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if saved.Owner != nil || saved.Nonce != "fresh-nonce" {
				t.Fatal("stale client mutated pairing")
			}
			bound, paired, err := c.prepareMessage(fresh, "new-user", "new-chat", "fresh-nonce")
			if err != nil || !paired || !bound.isAllowed("new-user") {
				t.Fatalf("fresh client rejected: paired=%v err=%v", paired, err)
			}
		})
	}
}

func TestSameCredentialsKeepCurrentConnection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{AppID: "app", AppSecret: "secret"}); err != nil {
		t.Fatal(err)
	}
	c := New(nil)
	runCtx, _ := c.base.Start(context.Background())
	defer c.base.Stop()
	client := newFeishuClient("app", "secret")
	c.api = client
	if _, err := c.addApp([]string{"app", "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.api != client || runCtx.Err() != nil {
		t.Fatal("unchanged credentials restarted the connection")
	}
}
