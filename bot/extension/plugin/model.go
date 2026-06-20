package plugin

import "time"

// Plugin represents an installed plugin instance.
type Plugin struct {
	Manifest
	Dir            string
	Source         string
	Enabled        bool
	InstalledAt    time.Time
	HasInstallStub bool
}

// SkillDirs returns the absolute skill directories for this plugin.
func (p *Plugin) SkillDirs() []string {
	var dirs []string
	for _, s := range p.Manifest.Skills {
		dirs = append(dirs, resolvePath(p.Dir, s))
	}
	if len(p.Manifest.Skills) == 0 {
		dirs = append(dirs, p.autoDiscoverSkills()...)
	}
	return dirs
}

// AgentPaths returns agent .md file paths declared or auto-discovered.
func (p *Plugin) AgentPaths() []string {
	if len(p.Manifest.Agents) > 0 {
		var paths []string
		for _, a := range p.Manifest.Agents {
			paths = append(paths, resolvePath(p.Dir, a))
		}
		return paths
	}
	return p.autoDiscoverAgents()
}

// HooksPath returns the hooks.json path and whether it exists.
func (p *Plugin) HooksPath() (string, bool) {
	if p.Manifest.Hooks != nil && p.Manifest.Hooks.Source != "" {
		return resolvePath(p.Dir, p.Manifest.Hooks.Source), true
	}
	return p.autoDiscoverHooks()
}

// MCPServers returns MCP server configs declared or auto-discovered.
func (p *Plugin) MCPServers() map[string]MCPServerConfig {
	if len(p.Manifest.MCPServers) > 0 {
		return p.Manifest.MCPServers
	}
	return p.autoDiscoverMCP()
}
