package mcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"nekocode/logger"
	utilhttp "nekocode/util/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

var errAuthorizationRequired = errors.New("MCP requires authorization; use the Authorize button or /mcp-login <name>")

type oauthHandler struct {
	ctx         context.Context
	cfg         ServerConfig
	name        string
	mu          sync.Mutex
	authorizeMu sync.Mutex
	source      oauth2.TokenSource
	loaded      bool
	status      string
	authURL     string
	// oauthClient applies the MCP-derived network policy to every OAuth side
	// request (discovery, DCR, token exchange, refresh), so a public MCP
	// server cannot redirect them at local services.
	oauthClient *http.Client
	interactive bool
	scopes      []string
	openBrowser func(string) error
	// closed marks the owning connection as torn down. A token exchange
	// still in flight must not persist credentials for a logout that is
	// about to (or already did) remove them.
	closed bool
}

func newOAuthHandler(ctx context.Context, name string, cfg ServerConfig) *oauthHandler {
	return &oauthHandler{ctx: ctx, cfg: cfg, name: name, oauthClient: oauthHTTPClient(cfg.URL), interactive: cfg.interactive, scopes: append([]string(nil), cfg.authorizationScopes...), openBrowser: func(raw string) error { return openAuthorizationBrowser(ctx, raw) }}
}
func (h *oauthHandler) state() (string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status, h.authURL
}
func (h *oauthHandler) setState(status, uri string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = status
	h.authURL = uri
}

// shutdown marks the handler's connection as torn down. remoteClient.Close
// calls it before the owner removes credentials, so an in-flight token
// exchange cannot recreate a login after logout.
func (h *oauthHandler) shutdown() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
}

func (h *oauthHandler) isClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}
func (h *oauthHandler) load() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loaded {
		return nil
	}
	// Explicit login must work even when persisted credentials are unusable.
	if h.interactive {
		h.restoreScopesLocked()
		h.loaded = true
		return nil
	}
	path, err := credentialPath(h.name, h.cfg)
	if err != nil {
		return err
	}
	saved, err := readCredential(path)
	if err != nil {
		return err
	}
	if saved != nil {
		h.scopes = unionOAuthScopes(h.scopes, saved.Config.Scopes)
		ctx := context.WithValue(h.ctx, oauth2.HTTPClient, h.oauthClient)
		h.source = &persistentTokenSource{source: saved.Config.TokenSource(ctx, &saved.Token), cfg: saved.Config, token: saved.Token, path: path, ctx: ctx, generation: saved.Generation}
	}
	h.loaded = true
	return nil
}
func (h *oauthHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	source := h.source
	h.mu.Unlock()
	if source == nil {
		return nil, nil
	}
	token, err := source.Token()
	if err != nil {
		if errors.Is(err, errAuthorizationRequired) {
			// The token source removes rejected grants while holding the store
			// lock. A different generation only invalidates this memory state.
			h.mu.Lock()
			if h.source == source {
				h.source = nil
				// Write both fields directly: h.mu is already held here and a
				// stale auth URL would hand the UI a dead callback link.
				h.status = StatusAuthRequired
				h.authURL = ""
			}
			h.mu.Unlock()
			return nil, nil
		}
		// Do not expose token endpoint response bodies, which may contain secrets.
		return nil, fmt.Errorf("MCP token refresh failed; retry or reauthorize")
	}
	return oauth2.StaticTokenSource(token), nil
}
func (h *oauthHandler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	if resp.StatusCode == http.StatusForbidden {
		challenges, _ := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
		insufficient := false
		for _, challenge := range challenges {
			if challenge.Scheme == "bearer" && challenge.Params["error"] == "insufficient_scope" {
				insufficient = true
			}
		}
		if !insufficient {
			resp.Body.Close()
			return fmt.Errorf("MCP access denied")
		}
	}
	h.rememberChallengeScopes(resp)
	h.authorizeMu.Lock()
	defer h.authorizeMu.Unlock()
	h.mu.Lock()
	interactive := h.interactive
	h.interactive = false
	h.mu.Unlock()
	if !interactive {
		resp.Body.Close()
		h.setState(StatusAuthRequired, "")
		return errAuthorizationRequired
	}
	defer func() {
		status, _ := h.state()
		if status == StatusAuthorizing {
			h.setState(StatusAuthRequired, "")
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	stop := context.AfterFunc(h.ctx, cancel)
	defer stop()
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(h.cfg.OAuthCallbackPort)))
	if err != nil {
		resp.Body.Close()
		return fmt.Errorf("start OAuth callback: %w", err)
	}
	defer listener.Close()
	callbackURL := "http://" + listener.Addr().String() + "/oauth/callback"
	config := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL: callbackURL, RequestRefreshToken: true, Client: h.oauthClient,
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "NekoCode", RedirectURIs: []string{callbackURL}, GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none",
		}},
	}
	if h.cfg.OAuthClientID != "" {
		credentials := &oauthex.ClientCredentials{ClientID: h.cfg.OAuthClientID}
		if h.cfg.OAuthClientSecret != "" {
			// Confidential client: GitHub-style servers that skip DCR demand
			// the secret at the token endpoint even with PKCE in play.
			credentials.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: h.cfg.OAuthClientSecret}
		}
		config.PreregisteredClient = credentials
	}
	if h.cfg.OAuthClientMetadataURL != "" {
		config.ClientIDMetadataDocumentConfig = &auth.ClientIDMetadataDocumentConfig{URL: h.cfg.OAuthClientMetadataURL}
	}
	config.AuthorizationCodeFetcher = func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		if err := utilhttp.ValidateSecureURL(args.URL); err != nil {
			return nil, err
		}
		return h.receiveCode(ctx, listener, args.URL)
	}
	config.NewTokenSource = func(_ context.Context, cfg *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
		if h.isClosed() {
			return nil, fmt.Errorf("MCP connection closed during authorization")
		}
		path, err := credentialPath(h.name, h.cfg)
		if err != nil {
			return nil, err
		}
		generation := rand.Text()
		if err := writeCredential(path, credentialRecord{Config: *cfg, Token: *token, Generation: generation}); err != nil {
			return nil, err
		}
		if h.isClosed() {
			// The connection was torn down while the token exchange finished.
			// Drop the credential this callback just wrote so the concurrent
			// logout cannot be silently resurrected by a later reconnect.
			_ = forgetCredential(h.name, h.cfg)
			return nil, fmt.Errorf("MCP connection closed during authorization")
		}
		refreshCtx := context.WithValue(h.ctx, oauth2.HTTPClient, h.oauthClient)
		source := &persistentTokenSource{source: cfg.TokenSource(refreshCtx, token), cfg: *cfg, token: *token, path: path, ctx: refreshCtx, generation: generation}
		h.mu.Lock()
		h.source = source
		h.mu.Unlock()
		return source, nil
	}
	handler, err := auth.NewAuthorizationCodeHandler(config)
	if err != nil {
		resp.Body.Close()
		logger.Log("mcp %s authorize setup: %v", h.name, err)
		return err
	}
	h.setState(StatusAuthorizing, "")
	if err := handler.Authorize(ctx, req, h.withAuthorizationScopes(resp)); err != nil {
		// SDK errors may include token exchange response bodies. Record only the
		// concrete error class; health and logs must not expose that body.
		logger.Log("%s", authorizationFailureLog(h.name, err))
		// SDK errors can include token exchange bodies. Keep them out of health/model output.
		if ctx.Err() != nil {
			return fmt.Errorf("MCP authorization cancelled or timed out")
		}
		return fmt.Errorf("MCP authorization failed; check client registration and retry")
	}
	h.setState("", "")
	return nil
}

func authorizationFailureLog(name string, err error) string {
	return fmt.Sprintf("mcp %s authorize failed (error_type=%T)", name, err)
}
func (h *oauthHandler) receiveCode(ctx context.Context, listener net.Listener, authorizationURL string) (*auth.AuthorizationResult, error) {
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		return nil, err
	}
	expected := parsed.Query().Get("state")
	if expected == "" {
		return nil, fmt.Errorf("missing OAuth state")
	}
	type callback struct {
		result *auth.AuthorizationResult
		err    error
	}
	result := make(chan callback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", 405)
			return
		}
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(expected)) != 1 {
			http.Error(w, "Invalid OAuth state", 400)
			return
		}
		reply := callback{result: &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}}
		if q.Get("error") != "" {
			reply.err = fmt.Errorf("authorization denied")
		}
		if reply.err == nil && reply.result.Code == "" {
			http.Error(w, "Missing authorization code", 400)
			return
		}
		select {
		case result <- reply:
			fmt.Fprint(w, "Authorization response received. Return to NekoCode to check the connection.")
		default:
			http.Error(w, "Callback already received", 409)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	h.setState(StatusAuthorizing, authorizationURL)
	// A failed desktop opener still leaves a usable manual URL in the UI/TUI.
	go func() { _ = h.openBrowser(authorizationURL) }()
	select {
	case reply := <-result:
		// The callback handler still has to finish writing the browser's
		// response. Close would abort that connection and intermittently EOF.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return reply.result, reply.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func forgetCredential(name string, cfg ServerConfig) error {
	path, err := credentialPath(name, cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return withCredentialLock(ctx, path, func() error {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	})
}
