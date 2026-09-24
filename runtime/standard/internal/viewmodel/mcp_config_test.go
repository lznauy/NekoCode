package viewmodel

import (
	"nekocode/bot/config"
	"reflect"
	"testing"
)

func TestRemoteMCPConfigRoundTrip(t *testing.T) {
	input := map[string]config.MCPServerConfig{"docs": {URL: "https://example.com/mcp", OAuthClientID: "public", OAuthClientSecret: "secret", OAuthClientMetadataURL: "https://example.com/client.json", OAuthCallbackPort: 18765, Enabled: true}}
	if got := mcpServerConfigsFromView(mcpServerConfigsToView(input)); !reflect.DeepEqual(got, input) {
		t.Fatalf("OAuth fields lost: %#v", got)
	}
}

func TestJevConfigRoundTrip(t *testing.T) {
	thresh := 0.4
	enabled := false
	input := config.Config{Jev: &config.JevConfig{APIKey: "key", BaseURL: "https://example.com", Model: "jev-test", KeepThreshold: &thresh, Enabled: &enabled}}
	if got := ToConfig(Config(input)); !reflect.DeepEqual(input.Jev, got.Jev) {
		t.Fatalf("Jev configuration lost: %+v", got.Jev)
	}
}
