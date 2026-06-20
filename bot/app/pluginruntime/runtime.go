package pluginruntime

import (
	"strings"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/debug"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/hooks"
	"nekocode/bot/plugincli"
	"nekocode/bot/tools"
)

type Runtime struct {
	Hooks      *hooks.Registry
	Tools      *tools.Registry
	MCPClients map[string]*mcp.Client
	Logf       func(string, ...any)
}

func (r Runtime) Load(p *plugin.Plugin) {
	r.registerAgents(p)
	r.registerHooks(p)
	r.registerMCP(p)
}

func (r Runtime) Unload(p *plugin.Plugin) {
	r.unregisterAgents(p)
	r.unregisterMCP(p)
	r.unregisterHooks(p)
}

func (r Runtime) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
		return
	}
	debug.Log(format, args...)
}

func (r Runtime) registerAgents(p *plugin.Plugin) {
	for _, agentPath := range p.AgentPaths() {
		def, err := subagent.ParseAgentMD(agentPath)
		if err != nil {
			r.logf("plugin: agent %s: %v", agentPath, err)
			continue
		}
		subagent.RegisterPlugin(def.ToAgentType())
	}
}

func (r Runtime) unregisterAgents(p *plugin.Plugin) {
	for _, ap := range p.AgentPaths() {
		def, err := subagent.ParseAgentMD(ap)
		if err == nil {
			subagent.UnregisterPlugin(def.Name)
		}
	}
}

func (r Runtime) registerHooks(p *plugin.Plugin) {
	if r.Hooks == nil {
		return
	}
	if hooksPath, ok := p.HooksPath(); ok {
		if pluginHooks, err := hooks.LoadPluginHooks(p.Dir, hooksPath); err == nil {
			for _, h := range pluginHooks {
				r.Hooks.Register(h)
			}
		} else {
			r.logf("plugin: hooks %s: %v", hooksPath, err)
		}
	}
}

func (r Runtime) unregisterHooks(p *plugin.Plugin) {
	if r.Hooks == nil {
		return
	}
	r.Hooks.UnregisterWhere(func(h hooks.Hook) bool {
		return strings.HasPrefix(h.Name, "plugin:") && strings.Contains(h.Name, p.Dir)
	})
}

func (r Runtime) registerMCP(p *plugin.Plugin) {
	if r.Tools == nil || r.MCPClients == nil {
		return
	}
	for name, cfg := range p.MCPServers() {
		level := mcp.ParseDangerLevel(cfg.DangerLevel)
		cfg.Env = plugincli.ExpandPluginEnv(cfg.Env, p.Dir)
		client := mcp.NewClient(name, cfg)
		if old, exists := r.MCPClients[name]; exists {
			old.Close()
		}
		r.MCPClients[name] = client

		if err := client.Start(); err != nil {
			r.logf("plugin: mcp %s start: %v", name, err)
			continue
		}
		mcpTools, err := client.ListTools()
		if err != nil {
			r.logf("plugin: mcp %s list tools: %v", name, err)
			continue
		}
		for _, td := range mcpTools {
			r.Tools.Register(mcp.NewMCPTool(client, td, level))
		}
	}
}

func (r Runtime) unregisterMCP(p *plugin.Plugin) {
	if r.Tools == nil || r.MCPClients == nil {
		return
	}
	for srvName := range p.MCPServers() {
		client, ok := r.MCPClients[srvName]
		if !ok {
			continue
		}
		for _, t := range r.Tools.List() {
			if IsMCPToolForClient(t.Name(), client.Name) {
				r.Tools.Unregister(t.Name())
			}
		}
		client.Close()
		delete(r.MCPClients, srvName)
	}
}

func IsMCPToolForClient(toolName, clientName string) bool {
	return strings.HasPrefix(toolName, clientName+"__")
}
