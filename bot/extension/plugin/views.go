package plugin

import (
	"path/filepath"

	"nekocode/common"
)

func (m *Manager) Views() []common.PluginView {
	plugins := m.reg.List()
	out := make([]common.PluginView, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, viewFor(p))
	}
	return out
}

func viewFor(p *Plugin) common.PluginView {
	return common.PluginView{
		Name:        p.Name,
		Version:     p.Version,
		Description: p.Description,
		Source:      p.Source,
		Dir:         p.Dir,
		Enabled:     p.Enabled,
		Skills:      p.SkillDirs(),
		SkillNames:  skillNamesFromDirs(p.SkillDirs()),
		Agents:      agentBaseNames(p.AgentPaths()),
		Commands:    commandNames(p.Manifest.Commands),
		MCPServers:  mcpServerNames(p.MCPServers()),
		HasHooks:    hooksExists(p),
	}
}

// MCPServersFor flattens the MCP servers declared by a plugin.
func MCPServersFor(p *Plugin) []common.MCPServerView {
	servers := p.MCPServers()
	out := make([]common.MCPServerView, 0, len(servers))
	for name, cfg := range servers {
		cfg = ExpandPluginMCPConfig(cfg, p.Dir)
		args := append([]string(nil), cfg.Args...)
		out = append(out, common.MCPServerView{
			Name:          name,
			Plugin:        p.Name,
			Command:       cfg.Command,
			Args:          args,
			DangerLevel:   cfg.DangerLevel,
			PluginEnabled: p.Enabled,
		})
	}
	return out
}

func skillNamesFromDirs(dirs []string) []string {
	var names []string
	for _, d := range dirs {
		if d == "" {
			continue
		}
		names = append(names, filepath.Base(d))
	}
	return names
}

func agentBaseNames(paths []string) []string {
	var names []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		names = append(names, filepath.Base(p))
	}
	return names
}

func commandNames(cmds []CommandEntry) []string {
	var names []string
	for _, c := range cmds {
		if c.Name == "" {
			continue
		}
		names = append(names, c.Name)
	}
	return names
}

func mcpServerNames(servers map[string]MCPServerConfig) []string {
	var names []string
	for name := range servers {
		names = append(names, name)
	}
	return names
}

func hooksExists(p *Plugin) bool {
	_, ok := p.HooksPath()
	return ok
}
