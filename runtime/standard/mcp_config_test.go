package standard

import (
	"nekocode/bot/extension/mcp"
	controlruntime "nekocode/runtime"
	"reflect"
	"testing"
)

func TestSessionMCPConfigPreservesOAuthSettings(t *testing.T) {
	input := controlruntime.MCPServerConfig{URL: "https://example.com/mcp", AllowInsecureHTTP: true, Headers: map[string]string{"X-Tenant": "tenant"}, OAuthClientID: "client", OAuthClientSecret: "secret", OAuthClientMetadataURL: "https://example.com/client", OAuthCallbackPort: 12345}
	got := sessionMCPConfigs([]controlruntime.MCPServerSpec{{Name: "docs", Config: input}})["docs"]
	want := mcp.ServerConfig{URL: input.URL, AllowInsecureHTTP: input.AllowInsecureHTTP, Headers: input.Headers, OAuthClientID: input.OAuthClientID, OAuthClientSecret: input.OAuthClientSecret, OAuthClientMetadataURL: input.OAuthClientMetadataURL, OAuthCallbackPort: input.OAuthCallbackPort}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("session MCP configuration lost connection settings")
	}
	got.Headers["X-Tenant"] = "changed"
	if input.Headers["X-Tenant"] != "tenant" {
		t.Fatal("session transport shares headers with its caller")
	}
}
