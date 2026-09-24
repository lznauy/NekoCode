package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func expiredCredentialFixture(t *testing.T) (*oauthFixture, ServerConfig, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	f := newOAuthFixture(t)
	cfg := ServerConfig{URL: f.server.URL + "/mcp"}
	path, _ := credentialPath("docs", cfg)
	record := credentialRecord{Config: oauth2.Config{ClientID: "fixture", Endpoint: oauth2.Endpoint{TokenURL: f.server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}, Token: oauth2.Token{AccessToken: "old", RefreshToken: "refresh-1", Expiry: time.Now().Add(-time.Hour)}}
	if err := writeCredential(path, record); err != nil {
		t.Fatal(err)
	}
	return f, cfg, path
}
func TestIndependentHandlersShareRefresh(t *testing.T) {
	f, cfg, _ := expiredCredentialFixture(t)
	first := newOAuthHandler(context.Background(), "docs", cfg)
	second := newOAuthHandler(context.Background(), "docs", cfg)
	for _, h := range []*oauthHandler{first, second} {
		if err := h.load(); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []*oauthHandler{first, second} {
		source, err := h.TokenSource(context.Background())
		if err != nil || source == nil {
			t.Fatalf("refresh: %v", err)
		}
	}
	if n := f.refreshes.Load(); n != 1 {
		t.Fatalf("two instances reused the same refresh token: %d refreshes", n)
	}
}
func TestLogoutInvalidatesOtherHandler(t *testing.T) {
	f, cfg, path := expiredCredentialFixture(t)
	other := newOAuthHandler(context.Background(), "docs", cfg)
	if err := other.load(); err != nil {
		t.Fatal(err)
	}
	if err := forgetCredential("docs", cfg); err != nil {
		t.Fatal(err)
	}
	source, err := other.TokenSource(context.Background())
	if err != nil || source != nil {
		t.Fatalf("logged-out instance reused credentials: source=%v err=%v", source, err)
	}
	if f.refreshes.Load() != 0 {
		t.Fatal("logout still allowed refresh")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("logout credentials resurrected")
	}
}

func TestStaleHandlerPreservesNewLogin(t *testing.T) {
	_, cfg, path := expiredCredentialFixture(t)
	old := newOAuthHandler(context.Background(), "docs", cfg)
	if err := old.load(); err != nil {
		t.Fatal(err)
	}
	saved, err := readCredential(path)
	if err != nil {
		t.Fatal(err)
	}
	saved.Generation = "new-login"
	saved.Token = oauth2.Token{AccessToken: "new-access", Expiry: time.Now().Add(time.Hour)}
	if err := writeCredential(path, *saved); err != nil {
		t.Fatal(err)
	}
	if source, err := old.TokenSource(context.Background()); err != nil || source != nil {
		t.Fatalf("old login must be invalidated: %v %v", source, err)
	}
	got, err := readCredential(path)
	if err != nil || got == nil || got.Generation != "new-login" {
		t.Fatalf("stale handler erased new login: %+v %v", got, err)
	}
}
