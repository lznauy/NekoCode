package viewmodel

import (
	"path/filepath"
	"sort"
	"strings"

	"nekocode/bot/config"
	extensionmgr "nekocode/bot/extension"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/skill"
	controlruntime "nekocode/runtime"
)

func Extension(snapshot extensionmgr.Snapshot, configuredMCP map[string]config.MCPServerConfig) controlruntime.SkillManagementView {
	plugins := pluginViews(snapshot.Plugins)
	for i := range plugins {
		plugins[i].AgentError = snapshot.AgentErrors[plugins[i].Name]
		for _, agent := range snapshot.Agents {
			if agent.Plugin == plugins[i].Name {
				plugins[i].ActiveAgents = append(plugins[i].ActiveAgents, agent.ID)
			}
		}
	}
	servers := pluginMCPViews(snapshot.Plugins)
	servers = append(servers, configMCPViews(configuredMCP)...)
	applyMCPHealth(servers, snapshot.MCPHealth)
	return controlruntime.SkillManagementView{
		Skills:  skillViews(snapshot.Skills, snapshot.LoadedSkills, plugins),
		Plugins: plugins,
		MCP:     servers,
	}
}

func configMCPViews(servers map[string]config.MCPServerConfig) []controlruntime.MCPServerView {
	out := make([]controlruntime.MCPServerView, 0, len(servers))
	for name, cfg := range servers {
		out = append(out, mcpServerView(name, "配置", cfg.Command, cfg.Args, cfg.Enabled))
	}
	return out
}

func mcpServerView(name, pluginName, command string, args []string, enabled bool) controlruntime.MCPServerView {
	view := controlruntime.MCPServerView{
		Name: name, Plugin: pluginName, Command: command,
		Args: append([]string(nil), args...), PluginEnabled: enabled,
	}
	if !enabled {
		view.Status = "disabled"
	}
	return view
}

func applyMCPHealth(servers []controlruntime.MCPServerView, health map[string]mcp.Health) {
	for i := range servers {
		if !servers[i].PluginEnabled {
			servers[i].Status = "disabled"
			continue
		}
		current, ok := health[servers[i].Name]
		if !ok {
			servers[i].Status = "unknown"
			continue
		}
		servers[i].Status = current.Status
		servers[i].Error = current.Error
		servers[i].ToolCount = current.ToolCount
	}
}

func pluginViews(plugins []*plugin.Plugin) []controlruntime.PluginView {
	out := make([]controlruntime.PluginView, 0, len(plugins))
	for _, p := range plugins {
		skillDirs := p.SkillDirs()
		out = append(out, controlruntime.PluginView{
			Name: p.Name, Version: p.Version, Description: p.Description,
			Source: p.Source, Dir: p.Dir, Enabled: p.Enabled,
			Skills: skillDirs, SkillNames: baseNames(skillDirs),
			Agents: baseNames(p.AgentPaths()), Commands: commandNames(p.Commands),
			MCPServers: mcpServerNames(p.MCPServers()), HasHooks: hasHooks(p),
		})
	}
	return out
}

func pluginMCPViews(plugins []*plugin.Plugin) []controlruntime.MCPServerView {
	var out []controlruntime.MCPServerView
	for _, p := range plugins {
		for name, cfg := range p.MCPServers() {
			cfg = plugin.ExpandPluginMCPConfig(cfg, p.Dir)
			out = append(out, mcpServerView(name, p.Name, cfg.Command, cfg.Args, p.Enabled))
		}
	}
	return out
}

func skillViews(skills []*skill.Skill, loaded map[string]bool, plugins []controlruntime.PluginView) []controlruntime.SkillView {
	out := make([]controlruntime.SkillView, 0, len(skills))
	for _, sk := range skills {
		kind, source, pluginName := sourceForDir(sk.Dir, plugins)
		files := append([]string(nil), sk.Files...)
		sort.Strings(files)
		out = append(out, controlruntime.SkillView{
			Name: sk.Name, Description: sk.Description, Dir: sk.Dir,
			Files: files, Loaded: loaded[sk.Name], Source: source,
			SourceKind: kind, Plugin: pluginName,
		})
	}
	return out
}

func sourceForDir(dir string, plugins []controlruntime.PluginView) (kind, label, pluginName string) {
	if dir == "" {
		return "builtin", "内置", ""
	}
	absDir := absOr(dir)
	for _, p := range plugins {
		for _, skillDir := range p.Skills {
			absSkillDir := absOr(skillDir)
			if absDir == absSkillDir || strings.HasPrefix(absDir, absSkillDir+string(filepath.Separator)) {
				return "plugin", "插件", p.Name
			}
		}
	}
	return "local", "本地", ""
}

func baseNames(paths []string) []string {
	var names []string
	for _, path := range paths {
		if path != "" {
			names = append(names, filepath.Base(path))
		}
	}
	return names
}

func commandNames(commands []plugin.CommandEntry) []string {
	var names []string
	for _, command := range commands {
		if command.Name != "" {
			names = append(names, command.Name)
		}
	}
	return names
}

func mcpServerNames(servers map[string]plugin.MCPServerConfig) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	return names
}

func hasHooks(p *plugin.Plugin) bool {
	_, ok := p.HooksPath()
	return ok
}

func absOr(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
