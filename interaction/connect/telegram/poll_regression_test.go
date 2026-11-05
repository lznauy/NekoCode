package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type pollTestTransport func(*http.Request) (*http.Response, error)

func (f pollTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPollDoesNotRestoreRevokedOwner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{ActiveProfile: "x", Profiles: []BotProfile{{Name: "x", BotToken: "token", Owner: &Device{UserID: 123}}}}); err != nil {
		t.Fatal(err)
	}
	c := New(nil)
	ctx, gen := c.base.Start(context.Background())
	defer c.base.Stop()
	entered := make(chan struct{})
	release := make(chan struct{})
	next := make(chan struct{})
	calls := 0
	client := newAPIClient("token")
	client.client = &http.Client{Transport: pollTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":[]}`)), Header: make(http.Header)}, nil
		}
		close(next)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	finished := make(chan struct{})
	go func() { defer close(finished); c.pollLoop(ctx, client, gen) }()
	defer func() { c.base.Stop(); <-finished }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no poll")
	}
	if _, err := c.unpairProfile(nil); err != nil {
		t.Fatal(err)
	}
	before, _ := loadConfig()
	if before.Profiles[0].Owner != nil {
		t.Fatal("unpair failed")
	}
	close(release)
	select {
	case <-next:
	case <-time.After(time.Second):
		t.Fatal("no second poll")
	}
	after, _ := loadConfig()
	if after.Profiles[0].Owner != nil {
		t.Fatalf("unpaired owner restored: %d", after.Profiles[0].Owner.UserID)
	}
}
func TestOldPollDoesNotClearNewClient(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{ActiveProfile: "x", Profiles: []BotProfile{{Name: "x", BotToken: "old"}}}); err != nil {
		t.Fatal(err)
	}
	c := New(nil)
	ctx, gen := c.base.Start(context.Background())
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	client := newAPIClient("old")
	client.client = &http.Client{Transport: pollTestTransport(func(r *http.Request) (*http.Response, error) { close(entered); <-release; return nil, context.Canceled })}
	go func() { c.pollLoop(ctx, client, gen); close(finished) }()
	<-entered
	newCtx, _ := c.base.Start(context.Background())
	defer c.base.Stop()
	_ = newCtx
	c.mu.Lock()
	c.active = "new"
	c.api = newAPIClient("new")
	c.mu.Unlock()
	close(release)
	<-finished
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != "new" || c.api == nil {
		t.Fatalf("stale exit erased new connector fields active=%q api=%v running=%v", c.active, c.api, c.base.IsRunning())
	}
}

func TestStaleConfigCannotRestoreOwner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{Profiles: []BotProfile{{Name: "x", BotToken: "token", Owner: &Device{UserID: 123}}}}); err != nil {
		t.Fatal(err)
	}
	stale, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(nil)
	if _, err := c.unpairProfile(nil); err != nil {
		t.Fatal(err)
	}
	stale.Profiles[0].UpdateOffset = 10
	if err := saveConfig(stale); !errors.Is(err, errConfigChanged) {
		t.Fatalf("stale write: %v", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles[0].Owner != nil {
		t.Fatal("owner restored")
	}
}

func TestPrepareUpdateUsesCurrentOwnerAndBot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{Profiles: []BotProfile{{Name: "x", BotToken: "token", Owner: &Device{UserID: 123}}}}); err != nil {
		t.Fatal(err)
	}
	c := New(nil)
	if _, err := c.unpairProfile(nil); err != nil {
		t.Fatal(err)
	}
	p, paired, err := c.preparePolledUpdate(context.Background(), newAPIClient("token"), "x", Update{UpdateID: 4, Message: &Message{From: &User{ID: 123}, Text: "/status"}})
	if err != nil {
		t.Fatal(err)
	}
	if paired || p.isAllowed(123) {
		t.Fatal("revoked owner remains authorized")
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles[0].Owner != nil || cfg.Profiles[0].UpdateOffset != 5 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if _, _, err := c.preparePolledUpdate(context.Background(), newAPIClient("old"), "x", Update{}); !errors.Is(err, errConfigChanged) {
		t.Fatalf("stale bot: %v", err)
	}
}

func TestConfigSnapshotRejectsChangedAndRestoredState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	original := Config{Profiles: []BotProfile{{Name: "x", BotToken: "token"}}}
	if err := saveConfig(original); err != nil {
		t.Fatal(err)
	}
	stale, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(Config{}); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(original); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(stale); !errors.Is(err, errConfigChanged) {
		t.Fatalf("stale snapshot accepted after reset: %v", err)
	}
}

func TestPreparePhotoCaptionPairingPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	profile := BotProfile{Name: "x", BotToken: "token"}
	profile.Start("nonce", time.Now(), time.Minute)
	if err := saveConfig(Config{Profiles: []BotProfile{profile}}); err != nil {
		t.Fatal(err)
	}
	_, paired, err := New(nil).preparePolledUpdate(context.Background(), newAPIClient("token"), "x", Update{UpdateID: 1, Message: &Message{From: &User{ID: 123}, Caption: "/start nonce", Photo: []PhotoSize{{FileID: "fixture"}}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !paired || !cfg.Profiles[0].isAllowed(123) || cfg.Profiles[0].Nonce != "" || cfg.Profiles[0].UpdateOffset != 2 {
		t.Fatalf("pairing not persisted: paired=%v config=%+v", paired, cfg)
	}
}
