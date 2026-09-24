package core

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"nekocode/bot/config"
	"nekocode/bot/extension"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/project"
	"nekocode/logger"
)

func (b *Bot) initExtensions() {
	b.ext = extension.New(extension.Config{
		ProjectRoot: b.cwd,
		Context:     b.ctxMgr, Tools: b.toolbox.Registry,
		Policy: b.policy, ContextWindow: b.cfg.EffectiveContextWindow(),
	})
	b.ext.SetMCPAuthNotifier(b.mcpAuthNotifier)

	b.initConfigMCPServers()
	b.ext.SetConfiguredMCPDefinitions(configuredMCPDefinitions(b.cfg, b.project))
	b.ext.Load()
}

// SetMCPAuthNotifier registers the sink for background MCP authorization
// outcomes (success or failure) so UIs can push them to the user. Re-applied
// automatically when the extension manager is rebuilt on config changes.
func (b *Bot) SetMCPAuthNotifier(fn func(message string)) {
	b.mu.Lock()
	b.mcpAuthNotifier = fn
	ext := b.ext
	b.mu.Unlock()
	if ext != nil {
		ext.SetMCPAuthNotifier(fn)
	}
}

func (b *Bot) initConfigMCPServers() {
	servers := b.effectiveMCPServers()
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		err := b.ext.AddMCPServerBackground(name, servers[name])
		if err != nil {
			logger.Log("config mcp %s: %v", name, err)
		}
	}
}

// effectiveMCPServers never mutates the configuration used by settings/save.
func (b *Bot) effectiveMCPServers() map[string]mcp.ServerConfig {
	return resolveMCPServers(b.cfg, b.project, b.cwd)
}

func resolveMCPServers(cfg *config.Config, project *project.Project, cwd string) map[string]mcp.ServerConfig {
	servers := make(map[string]mcp.ServerConfig)
	for name, cfg := range cfg.MCPServers {
		if cfg.Enabled {
			servers[name] = mcp.ServerConfig{Command: cfg.Command,
				URL: cfg.URL, Headers: maps.Clone(cfg.Headers), OAuthClientID: cfg.OAuthClientID, OAuthClientSecret: cfg.OAuthClientSecret, OAuthClientMetadataURL: cfg.OAuthClientMetadataURL, OAuthCallbackPort: cfg.OAuthCallbackPort,
				Args: append([]string(nil), cfg.Args...), Env: maps.Clone(cfg.Env), CWD: cwd}
		}
	}
	if project != nil {
		for name, cfg := range project.Servers {
			delete(servers, name)
			if cfg.Enabled {
				servers[name] = mcp.ServerConfig{Command: cfg.Command,
					URL: cfg.URL, Headers: maps.Clone(cfg.Headers), OAuthClientID: cfg.OAuthClientID, OAuthClientSecret: cfg.OAuthClientSecret, OAuthClientMetadataURL: cfg.OAuthClientMetadataURL, OAuthCallbackPort: cfg.OAuthCallbackPort,
					Args: append([]string(nil), cfg.Args...), Env: maps.Clone(cfg.Env), CWD: cfg.CWD}
			}
		}
	}
	return servers
}

func (b *Bot) SelectSkill(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	sk, ok := b.ext.Skill(name)
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	b.cmd.SelectSkill(b.ctxMgr, sk.Context)
	b.ext.MarkSkillLoaded(name)
	return nil
}

func (b *Bot) ClearSelectedSkill() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cmd.ClearSkill(b.ctxMgr)
}

func (b *Bot) Extensions() extension.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reloadView != nil {
		return *b.reloadView
	}
	return b.extensionsLocked()
}

// extensionsLocked captures a read-only view before or after a reload, never
// during its blocking work. Caller holds b.mu.
func (b *Bot) extensionsLocked() extension.Snapshot {
	snapshot := b.ext.Snapshot()
	snapshot.ConfiguredMCP = configuredMCPDefinitions(b.cfg, b.project)
	return snapshot
}

func configuredMCPDefinitions(cfg *config.Config, project *project.Project) []extension.ConfiguredMCP {
	definitions := make(map[string]extension.ConfiguredMCP)
	if cfg != nil {
		for name, cfg := range cfg.MCPServers {
			definitions[name] = extension.ConfiguredMCP{Name: name, Source: "配置",
				URL: cfg.URL, Command: cfg.Command, Args: append([]string(nil), cfg.Args...), Enabled: cfg.Enabled}
		}
	}
	if project != nil {
		for name, cfg := range project.Servers {
			definitions[name] = extension.ConfiguredMCP{Name: name, Source: project.MCPPath(),
				URL: cfg.URL, Command: cfg.Command, Args: append([]string(nil), cfg.Args...), Enabled: cfg.Enabled}
		}
	}
	configured := make([]extension.ConfiguredMCP, 0, len(definitions))
	for _, definition := range definitions {
		configured = append(configured, definition)
	}
	sort.Slice(configured, func(i, j int) bool {
		return configured[i].Name < configured[j].Name
	})
	return configured
}

// ReplaceSessionMCPServers atomically replaces transport-supplied MCP servers.
func (b *Bot) ReplaceSessionMCPServers(ctx context.Context, source string, configs map[string]mcp.ServerConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.ReplaceSessionMCPServers(ctx, source, configs)
}

func (b *Bot) SetPluginEnabled(name string, enabled bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.SetPluginEnabled(name, enabled)
}

func (b *Bot) RefreshExtensions() {
	b.reloadProject()
}

func (b *Bot) MCPAuthorizationAction(name, action string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.MCPAuthorizationAction(name, action)
}
