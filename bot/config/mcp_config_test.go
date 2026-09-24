package config

import "testing"

func TestValidateRemoteMCP(t *testing.T) {
	for _, tc := range []struct {
		name   string
		server MCPServerConfig
		valid  bool
	}{
		{"remote", MCPServerConfig{URL: "https://example.com/mcp", Enabled: true}, true},
		{"loopback", MCPServerConfig{URL: "http://127.0.0.1:1234/mcp", Enabled: true}, true},
		{"plaintext", MCPServerConfig{URL: "http://example.com/mcp", Enabled: true}, false},
		{"ambiguous", MCPServerConfig{URL: "https://example.com/mcp", Command: "npx", Enabled: true}, false},
		{"bad callback", MCPServerConfig{URL: "https://example.com/mcp", OAuthCallbackPort: -1, Enabled: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Active: "main", Models: []ModelConfig{{Name: "main", Provider: "openai", Model: "gpt-4o"}}, MCPServers: map[string]MCPServerConfig{"docs": tc.server}}
			if err := Validate(cfg); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
