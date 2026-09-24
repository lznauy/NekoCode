package mcp

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestExplicitLoginPreservesStepUpScopes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // Exercise the manual-browser fallback.
	f := newOAuthFixture(t)
	cfg := ServerConfig{URL: f.server.URL + "/mcp"}
	path, _ := credentialPath("docs", cfg)
	saved := credentialRecord{
		Config: oauth2.Config{ClientID: "fixture", Endpoint: oauth2.Endpoint{TokenURL: f.server.URL + "/token"}, Scopes: []string{"docs.archive", "docs.read"}},
		Token:  oauth2.Token{AccessToken: "access-1", RefreshToken: "refresh-1", Expiry: time.Now().Add(time.Hour)},
	}
	if err := writeCredential(path, saved); err != nil {
		t.Fatal(err)
	}
	m := New()
	defer m.Close()
	if err := m.AddBackground("config:docs", "docs", cfg); err != nil {
		t.Fatal(err)
	}
	waitScopeHealth(t, m, StatusReady)
	m.mu.Lock()
	h := m.servers["config:docs"].client.remote.auth
	m.mu.Unlock()
	// A tool needs a scope absent from the initialization challenge and PRM.
	req, _ := http.NewRequest(http.MethodPost, cfg.URL, nil)
	challenge := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Www-Authenticate": {`Bearer error="insufficient_scope", scope="docs.write"`}}, Body: io.NopCloser(strings.NewReader(""))}
	if err := h.Authorize(context.Background(), req, challenge); err == nil {
		t.Fatal("passive scope upgrade should require explicit authorization")
	}
	if err := m.AuthorizationAction("docs", "login"); err != nil {
		t.Fatal(err)
	}
	health := waitScopeHealth(t, m, StatusAuthorizing)
	u, err := url.Parse(health.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	scopes := strings.Fields(u.Query().Get("scope"))
	for _, want := range []string{"docs.read", "docs.archive", "docs.write", "offline_access"} {
		if !slices.Contains(scopes, want) {
			t.Fatalf("authorization lost scope %q: %v", want, scopes)
		}
	}
	if err := f.openBrowser(health.AuthURL); err != nil {
		t.Fatal(err)
	}
	waitScopeHealth(t, m, StatusReady)
	restored, err := readCredential(path)
	if err != nil || restored == nil || !slices.Contains(restored.Config.Scopes, "docs.write") || !slices.Contains(restored.Config.Scopes, "docs.archive") {
		t.Fatalf("upgraded scopes not persisted: %+v, %v", restored, err)
	}
}

func waitScopeHealth(t *testing.T, m *Manager, status string) Health {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h := m.Health()["docs"]
		if h.Status == status && (status != StatusAuthorizing || h.AuthURL != "") {
			return h
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %s: %+v", status, m.Health())
	return Health{}
}
