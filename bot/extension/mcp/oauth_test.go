package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

type oauthFixture struct {
	server        *httptest.Server
	refreshes     atomic.Int32
	registrations atomic.Int32
	browserOpens  atomic.Int32
	rejectRefresh atomic.Bool
	revoked       atomic.Bool
	mu            sync.Mutex
	challenge     string
}

func TestAuthorizationFailureLogDoesNotContainResponseBody(t *testing.T) {
	err := fmt.Errorf("token endpoint echoed client_secret=top-secret")
	got := authorizationFailureLog("docs", err)
	// Raw body and secret values must never appear verbatim; the message is
	// kept for diagnosis but credential-bearing fragments are redacted.
	if strings.Contains(got, "top-secret") || strings.Contains(got, err.Error()) {
		t.Fatalf("authorization log exposed error body: %q", got)
	}
	if !strings.Contains(got, "error_type=") || !strings.Contains(got, "client_secret=REDACTED") {
		t.Fatalf("authorization log lost classification or redacted detail: %q", got)
	}
}

func newOAuthFixture(t *testing.T) *oauthFixture {
	t.Helper()
	f := &oauthFixture{}
	mcpServer := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	mcpServer.AddTool(&sdk.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "connected"}}}, nil
	})
	mcpHandler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return mcpServer }, nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			json.NewEncoder(w).Encode(map[string]any{"resource": f.server.URL + "/mcp", "authorization_servers": []string{f.server.URL}, "scopes_supported": []string{"docs.read"}})
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token", "registration_endpoint": f.server.URL + "/register", "code_challenge_methods_supported": []string{"S256"}, "response_types_supported": []string{"code"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": []string{"docs.read", "offline_access"}})
		case "/register":
			f.registrations.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"client_id": "neko-fixture", "token_endpoint_auth_method": "none"})
		case "/authorize":
			f.browserOpens.Add(1)
			q := r.URL.Query()
			if q.Get("resource") != f.server.URL+"/mcp" || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" {
				t.Error("missing resource, PKCE or state")
			}
			if !strings.Contains(q.Get("scope"), "offline_access") {
				t.Error("missing offline_access")
			}
			f.mu.Lock()
			f.challenge = q.Get("code_challenge")
			f.mu.Unlock()
			callback, _ := url.Parse(q.Get("redirect_uri"))
			v := callback.Query()
			v.Set("state", q.Get("state"))
			v.Set("code", "good-code")
			callback.RawQuery = v.Encode()
			http.Redirect(w, r, callback.String(), http.StatusFound)
		case "/token":
			r.ParseForm()
			token := "access-1"
			refresh := "refresh-1"
			if r.Form.Get("grant_type") == "refresh_token" {
				f.refreshes.Add(1)
				if f.rejectRefresh.Load() {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":"invalid_grant"}`)
					return
				}
				token = "access-2"
				refresh = "refresh-2"
			} else {
				sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				f.mu.Lock()
				challenge := f.challenge
				f.mu.Unlock()
				if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("code") != "good-code" || r.Form.Get("resource") != f.server.URL+"/mcp" {
					t.Error("invalid code exchange")
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": token, "refresh_token": refresh, "expires_in": 3600, "token_type": "Bearer"})
		case "/mcp":
			if f.revoked.Load() || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, f.server.URL))
				w.WriteHeader(401)
				return
			}
			mcpHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	f.server = httptest.NewServer(handler)
	t.Cleanup(f.server.Close)
	return f
}
func (f *oauthFixture) openBrowser(raw string) error {
	resp, err := http.Get(raw)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
func TestRemoteOAuthRoundTripAndRefresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := newOAuthFixture(t)
	cfg := ServerConfig{URL: f.server.URL + "/mcp", interactive: true, CWD: t.TempDir()}
	client := newClient("docs", cfg)
	client.remote.auth.openBrowser = f.openBrowser
	t.Cleanup(func() { client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defs, err := client.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "echo" {
		t.Fatalf("tools: %+v", defs)
	}
	if out, err := client.CallTool(ctx, "echo", nil); err != nil || out != "connected" {
		t.Fatalf("call %q: %v", out, err)
	}
	path, _ := credentialPath("docs", cfg)
	saved, err := readCredential(path)
	if err != nil || saved == nil {
		t.Fatalf("credentials: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential permissions: %v", info.Mode())
	}
	client.Close()
	// Expiration simulates restart after time has elapsed without sleeping.
	saved.Token.Expiry = time.Now().Add(-time.Hour)
	if err := writeCredential(path, *saved); err != nil {
		t.Fatal(err)
	}
	cfg.interactive = false
	restored := newClient("docs", cfg)
	defer restored.Close()
	if _, err := restored.ListTools(ctx); err != nil {
		t.Fatal(err)
	}
	if f.browserOpens.Load() != 1 || f.refreshes.Load() != 1 {
		t.Fatalf("opens=%d refresh=%d", f.browserOpens.Load(), f.refreshes.Load())
	}
	saved, _ = readCredential(path)
	if saved.Token.RefreshToken != "refresh-2" {
		t.Fatal("rotated refresh token not persisted")
	}
	f.revoked.Store(true)
	if _, err := restored.CallTool(ctx, "echo", nil); err == nil {
		t.Fatal("revoked token accepted")
	}
	if status, _ := restored.remote.auth.state(); status != StatusAuthRequired {
		t.Fatalf("status=%s", status)
	}
}
func TestOAuthPassiveAndInvalidRefresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := newOAuthFixture(t)
	cfg := ServerConfig{URL: f.server.URL + "/mcp"}
	c := newClient("docs", cfg)
	defer c.Close()
	if err := c.Start(context.Background()); err == nil {
		t.Fatal("expected authorization required")
	}
	if status, _ := c.remote.auth.state(); status != StatusAuthRequired {
		t.Fatal(status)
	}
	if f.browserOpens.Load() != 0 || f.registrations.Load() != 0 {
		t.Fatal("passive connect prompted or registered")
	}
	path, _ := credentialPath("docs", cfg)
	record := credentialRecord{Config: oauth2.Config{ClientID: "fixture", Endpoint: oauth2.Endpoint{TokenURL: f.server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}, Token: oauth2.Token{AccessToken: "old", RefreshToken: "invalid", Expiry: time.Now().Add(-time.Hour)}}
	if err := writeCredential(path, record); err != nil {
		t.Fatal(err)
	}
	f.rejectRefresh.Store(true)
	c2 := newClient("docs", cfg)
	defer c2.Close()
	if err := c2.Start(context.Background()); err == nil {
		t.Fatal("expected reauthorization")
	}
	if status, _ := c2.remote.auth.state(); status != StatusAuthRequired {
		t.Fatal(status)
	}
}
func TestOAuthCallbackStateAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := newOAuthHandler(context.Background(), "docs", ServerConfig{})
	h.openBrowser = func(string) error { return fmt.Errorf("no browser") }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := h.receiveCode(ctx, listener, "https://example.com/authorize?state=expected")
		result <- err
	}()
	callback := "http://" + listener.Addr().String() + "/oauth/callback"
	var resp *http.Response
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		resp, err = http.Get(callback + "?state=wrong&code=attacker")
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("invalid state accepted: %d", resp.StatusCode)
	}
	select {
	case <-result:
		t.Fatal("invalid callback terminated flow")
	default:
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("callback did not cancel")
	}
	if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
		conn.Close()
		t.Fatal("callback listener leaked")
	}
}
func TestCredentialIsolation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := ServerConfig{URL: "https://example.com/mcp", CWD: "/project-a"}
	first, _ := credentialPath("docs", cfg)
	for _, other := range []ServerConfig{{URL: "https://other.com/mcp", CWD: cfg.CWD}, {URL: cfg.URL, CWD: "/project-b"}, {URL: cfg.URL, CWD: cfg.CWD, OAuthClientID: "other"}} {
		path, _ := credentialPath("docs", other)
		if path == first {
			t.Fatal("credential identity collision")
		}
	}
}

func TestExpiredWithoutRefreshCanLoginAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := newOAuthFixture(t)
	cfg := ServerConfig{URL: f.server.URL + "/mcp"}
	path, _ := credentialPath("docs", cfg)
	record := credentialRecord{Config: oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: f.server.URL + "/token"}}, Token: oauth2.Token{AccessToken: "expired", Expiry: time.Now().Add(-time.Hour)}}
	if err := writeCredential(path, record); err != nil {
		t.Fatal(err)
	}
	passive := newClient("docs", cfg)
	if err := passive.Start(context.Background()); err == nil {
		t.Fatal("expected authorization required")
	}
	if status, _ := passive.remote.auth.state(); status != StatusAuthRequired {
		t.Fatal(status)
	}
	passive.Close()
	cfg.interactive = true
	interactive := newClient("docs", cfg)
	defer interactive.Close()
	interactive.remote.auth.openBrowser = f.openBrowser
	if _, err := interactive.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.browserOpens.Load() != 1 {
		t.Fatal("explicit login did not open browser")
	}
}
func TestConcurrentTokenRefreshIsPersistedOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := newOAuthFixture(t)
	cfg := ServerConfig{URL: f.server.URL + "/mcp"}
	path, _ := credentialPath("docs", cfg)
	record := credentialRecord{Config: oauth2.Config{ClientID: "fixture", Endpoint: oauth2.Endpoint{TokenURL: f.server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}, Token: oauth2.Token{AccessToken: "old", RefreshToken: "refresh-1", Expiry: time.Now().Add(-time.Hour)}}
	if err := writeCredential(path, record); err != nil {
		t.Fatal(err)
	}
	h := newOAuthHandler(context.Background(), "docs", cfg)
	if err := h.load(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			source, err := h.TokenSource(context.Background())
			if err != nil || source == nil {
				t.Errorf("refresh: %v", err)
			}
		}()
	}
	wg.Wait()
	if f.refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d", f.refreshes.Load())
	}
	saved, _ := readCredential(path)
	if saved.Token.RefreshToken != "refresh-2" {
		t.Fatal("rotation lost")
	}
}
func TestLogoutClearsCredentialsWhenServerIsOffline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := newOAuthFixture(t)
	cfg := ServerConfig{URL: f.server.URL + "/mcp", interactive: true}
	c := newClient("docs", cfg)
	c.remote.auth.openBrowser = f.openBrowser
	defs, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := New()
	defer m.Close()
	m.servers["config:docs"] = &server{name: "docs", client: c, tools: defs}
	m.owners["docs"] = "config:docs"
	m.health["docs"] = Health{Status: StatusReady}
	f.server.Close()
	if err := m.AuthorizationAction("docs", "logout"); err != nil {
		t.Fatal(err)
	}
	path, _ := credentialPath("docs", cfg)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("credential survived logout: %v", err)
	}
}
func TestRemoteSessionReconnectsAfterClose(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := newOAuthFixture(t)
	cfg := ServerConfig{URL: f.server.URL + "/mcp", interactive: true}
	c := newClient("docs", cfg)
	defer c.Close()
	c.remote.auth.openBrowser = f.openBrowser
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.remote.mu.Lock()
	session := c.remote.session
	c.remote.mu.Unlock()
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		c.remote.mu.Lock()
		cleared := c.remote.session == nil
		c.remote.mu.Unlock()
		if cleared {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed session retained")
		}
		time.Sleep(time.Millisecond)
	}
	if out, err := c.CallTool(context.Background(), "echo", nil); err != nil || out != "connected" {
		t.Fatalf("reconnect: %q %v", out, err)
	}
	if f.browserOpens.Load() != 1 {
		t.Fatal("reconnect repeated authorization")
	}
}

func TestRemoteCloseCancelsStalledHTTP(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	started := make(chan struct{})
	release := make(chan struct{})
	cancelled := make(chan struct{})
	var startedOnce, cancelledOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume the POST body so net/http can detect the client disconnect.
		io.Copy(io.Discard, r.Body)
		startedOnce.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			cancelledOnce.Do(func() { close(cancelled) })
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	c := newClient("stalled", ServerConfig{URL: server.URL})
	done := make(chan error, 1)
	go func() { done <- c.Start(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close stuck on HTTP")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled connect succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("connect not cancelled")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("HTTP request survived close")
	}
}

func TestCancelAndLogoutReconnectPassively(t *testing.T) {
	for _, action := range []string{"cancel", "logout"} {
		t.Run(action, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			m := New()
			defer m.Close()
			// A connection constructed by explicit login carries interactive=true.
			cfg := ServerConfig{URL: "http://127.0.0.1:1/mcp", interactive: true}
			m.servers["config:docs"] = &server{name: "docs", client: newClient("docs", cfg)}
			m.owners["docs"] = "config:docs"
			if err := m.AuthorizationAction("docs", action); err != nil {
				t.Fatal(err)
			}
			m.mu.Lock()
			interactive := m.servers["config:docs"].client.remote.auth.interactive
			m.mu.Unlock()
			if interactive {
				t.Fatal("cancel/logout started another interactive login")
			}
		})
	}
}

func TestAuthorizationOptionsAreNotRetainedInDefinition(t *testing.T) {
	cfg := ServerConfig{URL: "https://example.com/mcp", interactive: true, authorizationScopes: []string{"docs.read"}}
	c := newClient("docs", cfg)
	defer c.Close()
	if !c.remote.auth.interactive || len(c.remote.auth.scopes) != 1 {
		t.Fatal("authorization options not applied")
	}
	if c.config.interactive || len(c.config.authorizationScopes) != 0 {
		t.Fatal("transient options retained in reusable definition")
	}
}

func TestBackgroundAuthorizationPublishesLink(t *testing.T) {
	m := New()
	defer m.Close()
	notices := make(chan string, 2)
	m.SetAuthNotifier(func(text string) { notices <- text })
	c := newClient("docs", ServerConfig{URL: "https://example.com/mcp"})
	m.mu.Lock()
	m.servers["config:docs"] = &server{name: "docs", client: c}
	m.owners["docs"] = "config:docs"
	m.health["docs"] = Health{Status: StatusStarting}
	m.mu.Unlock()
	c.remote.auth.setState(StatusAuthorizing, "https://example.com/authorize?state=test")
	select {
	case text := <-notices:
		if !strings.Contains(text, "https://example.com/authorize?state=test") {
			t.Fatal("notification omitted link")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("authorization link was not published")
	}
}

func TestBackgroundAuthorizationReportsDiscoveryFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	defer m.Close()
	notices := make(chan string, 2)
	m.SetAuthNotifier(func(text string) { notices <- text })
	if err := m.AddBackground("config:docs", "docs", ServerConfig{URL: "http://127.0.0.1:1/mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := m.AuthorizationAction("docs", "login"); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-notices:
		if !strings.Contains(text, "授权失败") {
			t.Fatalf("unexpected notification: %s", text)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("discovery failed silently")
	}
}

func TestLogoutWaitsForPendingCredentialWrite(t *testing.T) {
	_, cfg, path := expiredCredentialFixture(t)
	m := New()
	defer m.Close()
	c := newClient("docs", cfg)
	m.mu.Lock()
	m.servers["config:docs"] = &server{name: "docs", client: c}
	m.owners["docs"] = "config:docs"
	m.mu.Unlock()
	// Start holds remote.mu until its token callback finishes persisting.
	c.remote.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- m.AuthorizationAction("docs", "logout") }()
	select {
	case <-c.remote.ctx.Done():
	case <-time.After(time.Second):
		c.remote.mu.Unlock()
		t.Fatal("logout did not stop pending authorization")
	}
	err := writeCredential(path, credentialRecord{Config: oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: cfg.URL}}, Token: oauth2.Token{AccessToken: "last-token"}})
	c.remote.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("pending authorization resurrected credentials after logout")
	}
}

func TestOAuthCallbackDeliversCompleteBrowserResponse(t *testing.T) {
	for i := 0; i < 30; i++ {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		h := newOAuthHandler(context.Background(), "docs", ServerConfig{})
		browser := make(chan error, 1)
		h.openBrowser = func(string) error {
			resp, err := http.Get("http://" + listener.Addr().String() + "/oauth/callback?state=test&code=code")
			if err == nil {
				var body []byte
				body, err = io.ReadAll(resp.Body)
				resp.Body.Close()
				if err == nil && !strings.Contains(string(body), "Return to NekoCode") {
					err = fmt.Errorf("incomplete callback response")
				}
			}
			browser <- err
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err = h.receiveCode(ctx, listener, "https://example.com/authorize?state=test")
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if err := <-browser; err != nil {
			t.Fatalf("browser response %d: %v", i, err)
		}
	}
}
