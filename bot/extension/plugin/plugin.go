// Package plugin manages installed plugin manifests: discovery,
// install/uninstall, and enable/disable state.
//
// Manager is the package entry point. Activating plugin-provided skills,
// agents, hooks, and MCP servers belongs to the parent extension package.
package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nekocode/logger"
)

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
	if len(p.Skills) == 0 {
		return p.autoDiscoverSkills()
	}
	dirs := make([]string, 0, len(p.Skills))
	for _, s := range p.Skills {
		dirs = append(dirs, resolvePath(p.Dir, s))
	}
	return dirs
}

// AgentPaths returns agent .md file paths declared or auto-discovered.
func (p *Plugin) AgentPaths() []string {
	if len(p.Agents) > 0 {
		var paths []string
		for _, a := range p.Agents {
			paths = append(paths, resolvePath(p.Dir, a))
		}
		return paths
	}
	return p.autoDiscoverAgents()
}

// HooksPath returns the hooks.json path and whether it exists.
func (p *Plugin) HooksPath() (string, bool) {
	if p.Hooks != nil && p.Hooks.Source != "" {
		return p.Hooks.Source, true
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

// --- Auto-discovery ---
//
// When the manifest does not declare an extension explicitly, these methods
// find it by walking the plugin directory.

func (p *Plugin) autoDiscoverSkills() []string {
	return walkFind(p.Dir, "skills", true)
}

func (p *Plugin) autoDiscoverAgents() []string {
	var paths []string
	dirs := walkFind(p.Dir, "agents", true)
	for _, dir := range dirs {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				paths = append(paths, filepath.Join(dir, e.Name()))
			}
		}
	}
	return paths
}

func (p *Plugin) autoDiscoverHooks() (string, bool) {
	found := walkFind(p.Dir, "hooks.json", false)
	if len(found) > 0 {
		rel, _ := filepath.Rel(p.Dir, found[0])
		return rel, true
	}
	return "", false
}

func (p *Plugin) autoDiscoverMCP() map[string]MCPServerConfig {
	found := walkFind(p.Dir, ".mcp.json", false)
	for _, path := range found {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg struct {
			MCPServers map[string]MCPServerConfig `json:"mcpServers"`
		}
		if json.Unmarshal(data, &cfg) == nil && len(cfg.MCPServers) > 0 {
			return cfg.MCPServers
		}
	}
	return nil
}

// walkFind returns the paths under root whose base name matches matchName
// (case-insensitive). matchDir selects whether to match directories or
// files; the first match stops the descent into that subtree.
func walkFind(root string, matchName string, matchDir bool) []string {
	var results []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if matchDir && !d.IsDir() {
			return nil
		}
		if !matchDir && d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), matchName) {
			results = append(results, path)
			if matchDir {
				return filepath.SkipDir
			}
			return filepath.SkipAll
		}
		return nil
	})
	return results
}

func resolvePath(base, rel string) string {
	rel = strings.TrimPrefix(rel, "./")
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(base, rel)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Manager owns installed plugin discovery and persistent state. Runtime
// activation belongs to the parent extension manager.
type Manager struct {
	reg *registry
}

// New creates a plugin manager using the standard project and user plugin
// directories.
func New() *Manager {
	return NewWithDirs(defaultDirs())
}

// NewWithDirs fixes project and user plugin discovery paths at construction.
func NewWithDirs(dirs []string) *Manager {
	reg := newRegistry(append([]string(nil), dirs...))
	reg.Logf = logger.Log
	return &Manager{reg: reg}
}

// Load scans the plugin directories.
func (m *Manager) Load() {
	m.reg.LoadAll()
}

// Reload rescans the plugin directories.
func (m *Manager) Reload() {
	m.reg.LoadAll()
}

// ListPlugins returns all installed plugins sorted by name.
func (m *Manager) ListPlugins() []*Plugin {
	return m.reg.List()
}

// PluginSnapshots returns value copies of all installed plugins. Management
// views must use this instead of ListPlugins so a published snapshot does not
// alias Enabled, which Enable/Disable mutate in place.
func (m *Manager) PluginSnapshots() []*Plugin {
	return m.reg.Copies()
}

// SkillDirs returns all skill directories from enabled plugins.
func (m *Manager) SkillDirs() []string {
	return m.reg.SkillDirs()
}

// Get returns an installed plugin by name.
func (m *Manager) Get(name string) (*Plugin, bool) {
	return m.reg.Get(name)
}
