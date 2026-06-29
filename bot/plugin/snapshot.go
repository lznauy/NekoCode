package plugin

import "path/filepath"

type Snapshot struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Source      string   `json:"source,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Enabled     bool     `json:"enabled"`
	Skills      []string `json:"skills,omitempty"`
	SkillNames  []string `json:"skillNames,omitempty"`
	Agents      []string `json:"agents,omitempty"`
	Commands    []string `json:"commands,omitempty"`
	MCPServers  []string `json:"mcpServers,omitempty"`
	HasHooks    bool     `json:"hasHooks,omitempty"`
}

// MCPServerSnapshot is a flattened MCP server entry attributed to its source plugin.
type MCPServerSnapshot struct {
	Name          string   `json:"name"`
	Plugin        string   `json:"plugin"`
	Command       string   `json:"command"`
	Args          []string `json:"args,omitempty"`
	DangerLevel   string   `json:"dangerLevel,omitempty"`
	PluginEnabled bool     `json:"pluginEnabled"`
}

func (m *Manager) Snapshots() []Snapshot {
	plugins := m.reg.List()
	out := make([]Snapshot, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, snapshotFor(p))
	}
	return out
}

func snapshotFor(p *Plugin) Snapshot {
	return Snapshot{
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
func MCPServersFor(p *Plugin) []MCPServerSnapshot {
	servers := p.MCPServers()
	out := make([]MCPServerSnapshot, 0, len(servers))
	for name, cfg := range servers {
		args := append([]string(nil), cfg.Args...)
		out = append(out, MCPServerSnapshot{
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
