package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOAuthPublicMCPRejectsLocalDiscovery(t *testing.T) {
	var requests atomic.Int32
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer local.Close()
	client := oauthHTTPClient("https://remote.example/mcp")
	defer client.CloseIdleConnections()
	resp, err := client.Get(local.URL + "/side-effect")
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || requests.Load() != 0 {
		t.Fatalf("untrusted discovery reached local service: requests=%d err=%v", requests.Load(), err)
	}
}

func TestOAuthHandlerClientFollowsMCPPolicy(t *testing.T) {
	var requests atomic.Int32
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer local.Close()

	public := newOAuthHandler(context.Background(), "public", ServerConfig{URL: "https://remote.example/mcp"})
	_, err := public.oauthClient.Get(local.URL + "/side-effect")
	if err == nil || requests.Load() != 0 {
		t.Fatalf("public MCP OAuth client reached local service: requests=%d err=%v", requests.Load(), err)
	}

	localCfg := ServerConfig{URL: "http://" + strings.TrimPrefix(local.URL, "http://")}
	localHandler := newOAuthHandler(context.Background(), "local", localCfg)
	resp, err := localHandler.oauthClient.Get(local.URL + "/side-effect")
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil || requests.Load() != 1 {
		t.Fatalf("local MCP OAuth client should reach local endpoint: requests=%d err=%v", requests.Load(), err)
	}
}

func TestOAuthClientFollowsMethodPreservingRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET redirect changed method to %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landed", http.StatusMovedPermanently)
	}))
	defer source.Close()

	client := oauthHTTPClient(source.URL)
	defer client.CloseIdleConnections()
	resp, err := client.Get(source.URL + "/discover")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET 301 was not followed: status=%d", resp.StatusCode)
	}
}

func TestOAuthClientFollowsPOSTWith307(t *testing.T) {
	var body string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("307 redirect changed method to %s", r.Method)
		}
		body = r.PostFormValue("grant_type")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/token", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := oauthHTTPClient(source.URL)
	defer client.CloseIdleConnections()
	resp, err := client.PostForm(source.URL+"/token", map[string][]string{"grant_type": {"authorization_code"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST 307 was not followed: status=%d", resp.StatusCode)
	}
	if body != "authorization_code" {
		t.Fatalf("redirected token exchange lost its body: %q", body)
	}
}

func TestOAuthClientDoesNotFollowPOSTRewrittenToGET(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/token", http.StatusMovedPermanently)
	}))
	defer source.Close()

	client := oauthHTTPClient(source.URL)
	defer client.CloseIdleConnections()
	resp, err := client.PostForm(source.URL+"/token", map[string][]string{"grant_type": {"authorization_code"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// A POST 301 rewrites the method to GET, which would silently drop the
	// token exchange body: the client must return the 301 itself instead.
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("POST 301 must not be followed as GET: status=%d", resp.StatusCode)
	}
	if reached.Load() != 0 {
		t.Fatal("POST 301 was followed with a rewritten method")
	}
}

func TestOAuthClientStopsRedirectLoops(t *testing.T) {
	var hops atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		http.Redirect(w, r, r.URL.String(), http.StatusMovedPermanently)
	}))
	defer source.Close()

	client := oauthHTTPClient(source.URL)
	defer client.CloseIdleConnections()
	resp, err := client.Get(source.URL + "/discover")
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("self-referential redirect loop was not stopped")
	}
	if hops.Load() > 5 {
		t.Fatalf("redirect loop ran %d hops, want the 5-hop ceiling", hops.Load())
	}
}
