package app

import (
	"fmt"

	"nekocode/bot/extension/mcp"
	"nekocode/bot/plugin"
)

func (b *Bot) resetPluginMCPClients() {
	b.mcpClients = make(map[string]*mcp.Client)
}

func (b *Bot) closePluginMCPServers() {
	for name, client := range b.mcpClients {
		client.Close()
		delete(b.mcpClients, name)
	}
}

func (b *Bot) registerPluginMCPServer(pluginDir, name string, cfg plugin.MCPServerConfig) error {
	level := mcp.ParseDangerLevel(cfg.DangerLevel)
	cfg.Env = plugin.ExpandPluginEnv(cfg.Env, pluginDir)
	client := mcp.NewClient(name, cfg)
	if old, exists := b.mcpClients[name]; exists {
		old.Close()
	}
	b.mcpClients[name] = client

	if err := client.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	mcpTools, err := client.ListTools()
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	for _, td := range mcpTools {
		b.toolRegistry.Register(mcp.NewMCPTool(client, td, level))
	}
	return nil
}

func (b *Bot) unregisterPluginMCPServer(name string) {
	client, ok := b.mcpClients[name]
	if !ok {
		return
	}
	for _, t := range b.toolRegistry.List() {
		if plugin.IsMCPToolForClient(t.Name(), client.Name) {
			b.toolRegistry.Unregister(t.Name())
		}
	}
	client.Close()
	delete(b.mcpClients, name)
}
