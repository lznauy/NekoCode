package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/config"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/util/secrets"
)

func TestLoadedConfigurationCredentialsCannotEchoThroughManagedTools(t *testing.T) {
	b := newPersistTestBot(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := b.cfg.Clone()
	key := "managed-tool-config-credential-sentinel-8675309"
	cfg.Providers[0].APIKey = key
	imageSecret := "managed-image-secret-sentinel-8675309"
	oauthSecret := "managed-oauth-secret-sentinel-8675309"
	authToken := "managed-mcp-auth-token-sentinel-8675309"
	cfg.ImageGenModels[0].SecretKey = imageSecret
	cfg.MCPServers = map[string]config.MCPServerConfig{"remote": {
		URL: "https://example.com/mcp", OAuthClientSecret: oauthSecret,
		Headers: map[string]string{"Authorization": "Bearer " + authToken},
	}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	b.ag.Executor().SetFullAccess(true)
	if _, err := b.toolbox.Workspace().AddSessionRoot(filepath.Dir(config.Path()), workspace.AccessReadOnly); err != nil {
		t.Fatal(err)
	}
	quote := "'" + strings.ReplaceAll(config.Path(), "'", "'\\''") + "'"
	for _, call := range []toolcore.ToolCallItem{
		{ID: "shell-config", Name: "shell", Args: map[string]any{"command": "cat " + quote}},
		{ID: "read-config", Name: "read", Args: map[string]any{"path": config.Path(), "startLine": float64(1), "endLine": float64(500)}},
	} {
		r := b.ag.Executor().ExecuteBatch(t.Context(), []toolcore.ToolCallItem{call})[0]
		if r.Error != "" {
			t.Fatalf("%s failed: %s", call.Name, r.Error)
		}
		if strings.Contains(r.Output, key) || strings.Contains(r.Output, imageSecret) || strings.Contains(r.Output, oauthSecret) || strings.Contains(r.Output, authToken) || !strings.Contains(r.Output, secrets.Replacement) {
			t.Fatalf("%s exposed or lost credential protection", call.Name)
		}

		if call.Name == "read" {
			state := b.ag.Executor().ExecutionState()
			state.Reads.Observe(map[string]string{r.ReadID: r.Output})
			raw, err := os.ReadFile(config.Path())
			if err != nil {
				t.Fatal(err)
			}
			if state.Reads.Check(config.Path(), raw) == nil {
				t.Fatal("redacted preview authorized unseen original content")
			}
		}
	}
}
