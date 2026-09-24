package standard

import (
	"nekocode/bot/extension/mcp"
	controlruntime "nekocode/runtime"
	"reflect"
	"testing"
)

func TestSessionMCPConfigPreservesOAuthSettings(t *testing.T) {
	input := controlruntime.MCPServerConfig{URL: "https://example.com/mcp", OAuthClientID: "client", OAuthClientSecret: "secret", OAuthClientMetadataURL: "https://example.com/client", OAuthCallbackPort: 12345}
	got := sessionMCPConfigs([]controlruntime.MCPServerSpec{{Name: "docs", Config: input}})["docs"]
	want := mcp.ServerConfig{URL: input.URL, OAuthClientID: input.OAuthClientID, OAuthClientSecret: input.OAuthClientSecret, OAuthClientMetadataURL: input.OAuthClientMetadataURL, OAuthCallbackPort: input.OAuthCallbackPort}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("session MCP configuration lost OAuth settings")
	}
}
