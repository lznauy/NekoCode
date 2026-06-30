package app

import (
	"fmt"

	"nekocode/bot/config"
	"nekocode/bot/debug"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/common"
)

func (e *extensionFacade) InitConfigMCPServers(servers map[string]config.MCPServerConfig) {
	e.configMCP = nil
	for name, cfg := range servers {
		view := common.MCPServerView{
			Name:          name,
			Plugin:        "配置",
			Command:       cfg.Command,
			Args:          append([]string(nil), cfg.Args...),
			DangerLevel:   cfg.DangerLevel,
			PluginEnabled: cfg.Enabled,
			Status:        "disabled",
		}
		e.configMCP = append(e.configMCP, view)
		if !cfg.Enabled {
			continue
		}
		if err := e.registerConfigMCPServer(name, cfg); err != nil {
			debug.Log("config mcp %s: %v", name, err)
		}
	}
}

func (e *extensionFacade) resetPluginMCPClients() {
	e.mcpClients = make(map[string]*mcp.Client)
	e.mcpHealth = make(map[string]mcpHealth)
}

func (e *extensionFacade) closePluginMCPServers() {
	for name, client := range e.mcpClients {
		client.Close()
		delete(e.mcpClients, name)
	}
}

func (e *extensionFacade) registerPluginMCPServer(pluginDir, name string, cfg plugin.MCPServerConfig) error {
	level := mcp.ParseDangerLevel(cfg.DangerLevel)
	cfg = plugin.ExpandPluginMCPConfig(cfg, pluginDir)
	client := mcp.NewClient(name, cfg)
	return e.registerMCPClient(name, client, level)
}

func (e *extensionFacade) registerConfigMCPServer(name string, cfg config.MCPServerConfig) error {
	level := mcp.ParseDangerLevel(cfg.DangerLevel)
	client := mcp.NewClient(name, mcp.ServerConfig{
		Command:     cfg.Command,
		Args:        append([]string(nil), cfg.Args...),
		Env:         cfg.Env,
		DangerLevel: cfg.DangerLevel,
	})
	return e.registerMCPClient(name, client, level)
}

func (e *extensionFacade) registerMCPClient(name string, client *mcp.Client, level common.DangerLevel) error {
	if old, exists := e.mcpClients[name]; exists {
		old.Close()
	}
	e.mcpClients[name] = client
	e.mcpHealth[name] = mcpHealth{Status: "starting"}

	if err := client.Start(); err != nil {
		e.mcpHealth[name] = mcpHealth{Status: "error", Error: err.Error()}
		return fmt.Errorf("start: %w", err)
	}
	mcpTools, err := client.ListTools()
	if err != nil {
		_ = client.Close()
		delete(e.mcpClients, name)
		e.mcpHealth[name] = mcpHealth{Status: "error", Error: err.Error()}
		return fmt.Errorf("list tools: %w", err)
	}
	for _, td := range mcpTools {
		e.toolRegistry.Register(mcp.NewMCPTool(client, td, level))
	}
	e.mcpHealth[name] = mcpHealth{Status: "ready", ToolCount: len(mcpTools)}
	return nil
}

func (e *extensionFacade) unregisterPluginMCPServer(name string) {
	client, ok := e.mcpClients[name]
	if !ok {
		return
	}
	for _, t := range e.toolRegistry.List() {
		if plugin.IsMCPToolForClient(t.Name(), client.Name) {
			e.toolRegistry.Unregister(t.Name())
		}
	}
	client.Close()
	delete(e.mcpClients, name)
	delete(e.mcpHealth, name)
}
