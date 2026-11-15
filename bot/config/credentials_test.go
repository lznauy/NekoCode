package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/util/secrets"
)

func TestConfigurationRegistersCredentialFields(t *testing.T) {
	for _, operation := range []string{"save", "load"} {
		t.Run(operation, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			cfg := Default.Clone()
			cfg.Providers[0].APIKey = operation + "-provider-credential-sentinel"
			cfg.ImageGenModels[0].APIKey = operation + "-image-api-credential-sentinel"
			cfg.ImageGenModels[0].SecretKey = operation + "-image-secret-credential-sentinel"
			cfg.ImageUnderstandModels = []ImageUnderstandConfig{{Name: "vision", Provider: "openai", Model: "vision", APIKey: operation + "-vision-credential-sentinel"}}
			cfg.MCPServers = map[string]MCPServerConfig{"remote": {
				URL: "https://example.com/mcp", OAuthClientSecret: operation + "-oauth-credential-sentinel",
				Env: map[string]string{"API_KEY": operation + "-mcp-env-credential-sentinel", "LANG": "ordinary-language-value"},
				Headers: map[string]string{
					"api-key":       operation + "-mcp-header-credential-sentinel",
					"aUtHoRiZaTiOn": "Bearer " + operation + "-mcp-auth-credential-sentinel",
					"X-API-Key":     operation + "-mcp-x-api-credential-sentinel",
					"Accept":        "application/ordinary-credential-test",
				},
			}}
			cfg.Jev = &JevConfig{APIKey: operation + "-jev-credential-sentinel"}
			if operation == "save" {
				if err := Save(cfg); err != nil {
					t.Fatal(err)
				}
			} else {
				data, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(Path(), data, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Load(); err != nil {
					t.Fatal(err)
				}
			}
			for _, value := range []string{cfg.Providers[0].APIKey, cfg.ImageGenModels[0].APIKey, cfg.ImageGenModels[0].SecretKey, cfg.ImageUnderstandModels[0].APIKey, cfg.MCPServers["remote"].OAuthClientSecret, cfg.MCPServers["remote"].Env["API_KEY"], cfg.MCPServers["remote"].Headers["api-key"], cfg.Jev.APIKey} {
				if got := secrets.Redact(value); got != secrets.Replacement {
					t.Errorf("credential was not registered: %q", got)
				}
			}
			for _, value := range []string{
				cfg.MCPServers["remote"].Headers["aUtHoRiZaTiOn"], operation + "-mcp-auth-credential-sentinel",
				cfg.MCPServers["remote"].Headers["X-API-Key"],
			} {
				if got := secrets.Redact(value); got != secrets.Replacement {
					t.Errorf("HTTP credential was not registered: %q", got)
				}
			}
			if got := secrets.Redact(cfg.MCPServers["remote"].Headers["Accept"]); got != "application/ordinary-credential-test" {
				t.Errorf("ordinary HTTP header was registered: %q", got)
			}
			if got := secrets.Redact(cfg.MCPServers["remote"].Env["LANG"]); got != "ordinary-language-value" {
				t.Errorf("ordinary configuration value was registered: %q", got)
			}
		})
	}
}

func TestShortCredentialWarningDoesNotExposeValues(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cfg := &Config{
		Providers: []ProviderConfig{{APIKey: "短凭据甲"}},
		MCPServers: map[string]MCPServerConfig{"remote": {
			Headers: map[string]string{"Authorization": "Bearer 短凭据乙", "Accept": "text/plain"},
			Env:     map[string]string{"API_KEY": "短凭据丙"},
		}},
	}
	cfg.registerCredentials()
	output := logs.String()
	for _, field := range []string{"providers[0].api_key", "Authorization", "API_KEY"} {
		if !strings.Contains(output, field) {
			t.Errorf("missing warning for field %s", field)
		}
	}
	if strings.Count(output, "\n") != 3 {
		t.Fatalf("expected exactly three short credential warnings, got %d", strings.Count(output, "\n"))
	}
	for _, value := range []string{"短凭据甲", "短凭据乙", "短凭据丙", "Bearer 短凭据乙"} {
		if strings.Contains(output, value) {
			t.Fatal("warning exposed a credential")
		}
		if secrets.Redact(value) != secrets.Replacement {
			t.Fatal("short credential was not protected")
		}
	}
	logs.Reset()
	registerCredential("empty", "")
	registerCredential("long", "long-credential-warning-test")
	registerCredential("eight_characters", "边界凭据长度八字")
	if logs.Len() != 0 {
		t.Fatal("empty or sufficiently long credentials produced a warning")
	}
}

func TestFailedConfigurationDoesNotRegisterCredentials(t *testing.T) {
	for _, operation := range []string{"load", "save"} {
		t.Run(operation, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			cfg := Default.Clone()
			credential := "rejected-" + operation + "-credential-sentinel"
			cfg.Providers[0].APIKey = credential
			if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
				t.Fatal(err)
			}
			if operation == "load" {
				cfg.Active = "missing/model"
				data, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(Path(), data, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Load(); err == nil {
					t.Fatal("invalid configuration loaded")
				}
			} else {
				// A directory at the destination makes the atomic rename fail.
				if err := os.Mkdir(Path(), 0700); err != nil {
					t.Fatal(err)
				}
				if err := Save(cfg); err == nil {
					t.Fatal("save unexpectedly succeeded")
				}
			}
			if got := secrets.Redact(credential); got != credential {
				t.Fatal("failed configuration changed global output filtering")
			}
		})
	}
}

func TestPrefixedMCPCredentials(t *testing.T) {
	credentials := map[string]string{
		"GITHUB_TOKEN":         "review-github-credential-sentinel",
		"BRAVE_API_KEY":        "review-brave-credential-sentinel",
		"vendor-client-secret": "review-client-credential-sentinel",
		"Vendor_Access_Token":  "review-access-credential-sentinel",
		"APIKey":               "review-legacy-credential-sentinel",
	}
	ordinary := map[string]string{
		"TOKEN_LIMIT":      "review-token-limit-value",
		"API_KEY_FILE":     "review-key-file-value",
		"VENDOR_TOKEN_TTL": "review-token-ttl-value",
		"NOTTOKEN":         "review-no-boundary-value",
		"MONKEY":           "review-ordinary-key-value",
	}
	env := make(map[string]string)
	for key, value := range credentials {
		env[key] = value
	}
	for key, value := range ordinary {
		env[key] = value
	}
	cfg := Config{MCPServers: map[string]MCPServerConfig{"test": {
		Env:     env,
		Headers: map[string]string{"X-Custom-Token": "review-header-credential-sentinel", "X-Token-Limit": "review-header-limit-value"},
	}}}
	cfg.registerCredentials()
	for key, value := range credentials {
		if got := secrets.Redact("output: " + value); got != "output: "+secrets.Replacement {
			t.Errorf("MCP credential %s was not protected", key)
		}
	}
	for key, value := range ordinary {
		if secrets.Redact(value) != value {
			t.Errorf("ordinary MCP value %s was registered", key)
		}
	}
	if secrets.Redact("review-header-credential-sentinel") != secrets.Replacement {
		t.Error("custom credential header was not protected")
	}
	if secrets.Redact("review-header-limit-value") != "review-header-limit-value" {
		t.Error("ordinary header was registered")
	}
}
