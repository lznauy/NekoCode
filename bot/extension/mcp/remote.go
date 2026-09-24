package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	utilhttp "nekocode/util/http"
)

// Remote requests never forward credentials through redirects.
func remoteHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: endpointTransport{base: http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func mcpHTTPClient(ctx context.Context) *http.Client {
	c := remoteHTTPClient()
	// SSE tool responses may outlive ordinary OAuth HTTP round trips.
	c.Timeout = 0
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport := base.Clone()
		c.Transport = endpointTransport{base: transport, lifetime: ctx}
	} else {
		c.Transport = endpointTransport{base: http.DefaultTransport, lifetime: ctx}
	}
	return c
}

type endpointTransport struct {
	base     http.RoundTripper
	lifetime context.Context
}

func (t endpointTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := utilhttp.ValidateSecureURL(r.URL.String()); err != nil {
		return nil, err
	}
	if t.lifetime == nil {
		return t.base.RoundTrip(r)
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(t.lifetime, cancel)
	cleanup := func() { stop(); cancel() }
	resp, err := t.base.RoundTrip(r.Clone(ctx))
	if err != nil {
		cleanup()
		return nil, err
	}
	resp.Body = &lifetimeBody{ReadCloser: resp.Body, cleanup: cleanup}
	return resp, nil
}

func (t endpointTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type lifetimeBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *lifetimeBody) Close() error { defer b.cleanup(); return b.ReadCloser.Close() }

type remoteClient struct {
	mu         sync.Mutex
	cfg        ServerConfig
	session    *sdk.ClientSession
	ctx        context.Context
	cancel     context.CancelFunc
	auth       *oauthHandler
	httpClient *http.Client
}

func newRemoteClient(name string, cfg ServerConfig) *remoteClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &remoteClient{cfg: cfg, ctx: ctx, cancel: cancel, auth: newOAuthHandler(ctx, name, cfg), httpClient: mcpHTTPClient(ctx)}
}
func (r *remoteClient) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		return nil
	}
	if r.cfg.Command != "" {
		return fmt.Errorf("MCP server must specify command or URL, not both")
	}
	if err := utilhttp.ValidateSecureURL(r.cfg.URL); err != nil {
		return err
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	connectCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
	if err := r.auth.load(); err != nil {
		return err
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "NekoCode", Version: "0.1.0"}, nil)
	session, err := client.Connect(connectCtx, &sdk.StreamableClientTransport{
		Endpoint: r.cfg.URL, HTTPClient: r.httpClient, OAuthHandler: r.auth, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return err
	}
	r.session = session
	go func() {
		err := session.Wait()
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.session == session {
			r.session = nil
			if err != nil && r.ctx.Err() == nil {
				if status, _ := r.auth.state(); status == "" {
					r.auth.setState(StatusError, "")
				}
			}
		}
	}()
	if status, _ := r.auth.state(); status == StatusError {
		r.auth.setState("", "")
	}
	return nil
}
func (r *remoteClient) Close() error {
	// Mark the auth handler dead before cancelling: an in-flight token
	// exchange must not persist credentials that a logout is about to remove.
	r.auth.shutdown()
	r.cancel()
	defer r.httpClient.CloseIdleConnections()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		err := r.session.Close()
		r.session = nil
		return err
	}
	return nil
}
func (r *remoteClient) getSession(ctx context.Context) (*sdk.ClientSession, error) {
	if err := r.Start(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return nil, fmt.Errorf("MCP connection closed")
	}
	return r.session, nil
}
func (r *remoteClient) ListTools(ctx context.Context) ([]toolDef, error) {
	session, err := r.getSession(ctx)
	if err != nil {
		return nil, err
	}
	var defs []toolDef
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(tool)
		if err != nil {
			return nil, err
		}
		var def toolDef
		if err := json.Unmarshal(data, &def); err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, nil
}
func (r *remoteClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	session, err := r.getSession(ctx)
	if err != nil {
		return "", err
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return parseToolCallResult(data)
}
