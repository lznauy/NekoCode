package viewmodel

import (
	"nekocode/bot/config"
	"reflect"
	"testing"
)

func TestRemoteMCPConfigRoundTrip(t *testing.T) {
	input := map[string]config.MCPServerConfig{"docs": {URL: "https://example.com/mcp", Headers: map[string]string{"X-Tenant": "tenant"}, OAuthClientID: "public", OAuthClientSecret: "secret", OAuthClientMetadataURL: "https://example.com/client.json", OAuthCallbackPort: 18765, Enabled: true}}
	view := mcpServerConfigsToView(input)
	got := mcpServerConfigsFromView(view)
	if !reflect.DeepEqual(got, input) {
		t.Fatal("remote configuration changed after round trip")
	}
	view["docs"].Headers["X-Tenant"] = "changed"
	if input["docs"].Headers["X-Tenant"] != "tenant" || got["docs"].Headers["X-Tenant"] != "tenant" {
		t.Fatal("editable configuration view shares headers with runtime configuration")
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
