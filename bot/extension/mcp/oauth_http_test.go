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
