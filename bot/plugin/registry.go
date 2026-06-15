package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"nekocode/common"
)

// Plugin represents an installed plugin instance.
type Plugin struct {
	Manifest
	Dir            string
	Source         string
	Enabled        bool
	InstalledAt    time.Time
	HasInstallStub bool // install.sh detected in plugin root
}

// SkillDir returns the absolute skill directories for this plugin.
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

// AgentPaths returns agent .md file paths (declared or auto-discovered).
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

// MCPServers returns MCP server configs (declared or auto-discovered).
func (p *Plugin) MCPServers() map[string]MCPServerConfig {
	if len(p.Manifest.MCPServers) > 0 {
		return p.Manifest.MCPServers
	}
	return p.autoDiscoverMCP()
}

// --- recursive auto-discovery ----------------------------------------------

// walkFind walks root looking for entries whose name matches matchName (case-insensitive).
// If matchDir is true, only directories are matched; otherwise only files.
// For directories, the matched dir is not recursed into.
func walkFind(root string, matchName string, matchDir bool) []string {
	var results []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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

func resolvePath(base, rel string) string {
	rel = strings.TrimPrefix(rel, "./")
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(base, rel)
}

// registryJSON is the on-disk format for ~/.nekocode/plugins/registry.json.
type registryJSON struct {
	Plugins map[string]registryEntry `json:"plugins"`
}

type registryEntry struct {
	Version     string `json:"version"`
	Source      string `json:"source"`
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installedAt"`
}

// Registry manages plugin lifecycle.
type Registry struct {
	mu       sync.RWMutex
	plugins  map[string]*Plugin
	baseDirs []string

	Logf func(string, ...any)
}

// DefaultDirs returns plugin search paths (project > user).
func DefaultDirs() []string {
	return common.NekocodeDirs("plugins")
}

// userPluginDir returns the user-level plugin directory.
func userPluginDir() (string, error) {
	return filepath.Join(common.NekocodeHome(), "plugins"), nil
}

// NewRegistry creates a plugin registry scanning baseDirs.
func NewRegistry(baseDirs []string) *Registry {
	return &Registry{
		plugins:  make(map[string]*Plugin),
		baseDirs: baseDirs,
	}
}

// LoadAll scans all base dirs and loads plugin manifests.
// Returns all skill directories from enabled plugins.
func (r *Registry) LoadAll() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var skillDirs []string

	// Load registry.json from user dir to get enabled/disabled state + sources.
	regData := r.loadRegistryFile()

	seen := make(map[string]bool)
	for _, baseDir := range r.baseDirs {
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pluginDir := filepath.Join(baseDir, entry.Name())
			if !HasManifest(pluginDir) {
				continue
			}
			m, err := ParseManifest(pluginDir)
			if err != nil {
				if r.Logf != nil {
					r.Logf("plugin: skip %s: %v", pluginDir, err)
				}
				continue
			}
			if seen[m.Name] {
				continue // project-level overrides user-level
			}
			seen[m.Name] = true

			// Restore persisted metadata.
			enabled := true
			source := ""
			var installedAt time.Time
			if re, ok := regData.Plugins[m.Name]; ok {
				enabled = re.Enabled
				source = re.Source
				if t, err := time.Parse(time.RFC3339, re.InstalledAt); err == nil {
					installedAt = t
				}
			}

			p := &Plugin{
				Manifest:        *m,
				Dir:             pluginDir,
				Source:          source,
				Enabled:         enabled,
				InstalledAt:     installedAt,
				HasInstallStub:  fileExists(filepath.Join(pluginDir, "install.sh")),
			}

			r.plugins[m.Name] = p

			if enabled {
				skillDirs = append(skillDirs, p.SkillDirs()...)
			}

			if r.Logf != nil {
				r.Logf("plugin: loaded %s v%s (enabled=%v) from %s", m.Name, m.Version, enabled, pluginDir)
			}
		}
	}

	return skillDirs
}

// PreviewFromPath creates a Plugin from a local path without installing.
func (r *Registry) PreviewFromPath(source string) (*Plugin, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	if !HasManifest(abs) {
		return nil, fmt.Errorf("no .claude-plugin/plugin.json found in %s", abs)
	}
	m, err := ParseManifest(abs)
	if err != nil {
		return nil, err
	}
	return &Plugin{
		Manifest:       *m,
		Dir:            abs,
		Source:         source,
		Enabled:        true,
		InstalledAt:    time.Now(),
		HasInstallStub: fileExists(filepath.Join(abs, "install.sh")),
	}, nil
}

// Install clones or copies a plugin from source into user plugin dir.
func (r *Registry) Install(source string) (*Plugin, error) {
	userDir, err := userPluginDir()
	if err != nil {
		return nil, fmt.Errorf("user plugin dir: %w", err)
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return nil, fmt.Errorf("create plugin dir: %w", err)
	}

	var pluginDir string

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") ||
		strings.Contains(source, "github.com") || strings.Contains(source, "gitlab.com") {
		// Git URL: clone.
		name := repoName(source)
		pluginDir = filepath.Join(userDir, name)
		if err := r.gitClone(source, pluginDir); err != nil {
			return nil, err
		}
	} else if common.LooksLikeGit(source) {
		// Short form: "user/repo".
		url := "https://github.com/" + source
		name := strings.ReplaceAll(source, "/", "-")
		pluginDir = filepath.Join(userDir, name)
		if err := r.gitClone(url, pluginDir); err != nil {
			return nil, err
		}
	} else {
		// Local path: copy directory.
		abs, err := filepath.Abs(source)
		if err != nil {
			return nil, fmt.Errorf("resolve path: %w", err)
		}
		if !HasManifest(abs) {
			return nil, fmt.Errorf("no .claude-plugin/plugin.json found in %s", abs)
		}
		m, err := ParseManifest(abs)
		if err != nil {
			return nil, fmt.Errorf("parse manifest: %w", err)
		}
		pluginDir = filepath.Join(userDir, m.Name)
		if err := copyDir(abs, pluginDir); err != nil {
			return nil, fmt.Errorf("copy plugin: %w", err)
		}
	}

	m, err := ParseManifest(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("parse installed manifest: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	p := &Plugin{
		Manifest:       *m,
		Dir:            pluginDir,
		Source:         source,
		Enabled:        true,
		InstalledAt:    time.Now(),
		HasInstallStub: fileExists(filepath.Join(pluginDir, "install.sh")),
	}
	r.plugins[m.Name] = p
	r.saveRegistryFile()
	return p, nil
}

// Uninstall removes a plugin from disk and registry.
func (r *Registry) Uninstall(name string) error {
	r.mu.RLock()
	p, ok := r.plugins[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

	// Delete from disk first — if this fails, the in-memory state stays consistent.
	if err := os.RemoveAll(p.Dir); err != nil {
		return fmt.Errorf("remove plugin dir: %w", err)
	}

	r.mu.Lock()
	delete(r.plugins, name)
	r.saveRegistryFile()
	r.mu.Unlock()
	return nil
}

// List returns all installed plugins sorted by name.
func (r *Registry) List() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// Get returns a plugin by name.
func (r *Registry) Get(name string) (*Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// Enable enables a plugin.
func (r *Registry) Enable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	p.Enabled = true
	r.saveRegistryFile()
	return nil
}

// Disable disables a plugin without removing it.
func (r *Registry) Disable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	p.Enabled = false
	r.saveRegistryFile()
	return nil
}

// SkillDirs returns all skill directories from enabled plugins.
func (r *Registry) SkillDirs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var dirs []string
	for _, p := range r.plugins {
		if p.Enabled {
			dirs = append(dirs, p.SkillDirs()...)
		}
	}
	return dirs
}

// --- helpers ---

func (r *Registry) registryPath() (string, error) {
	dir, err := userPluginDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "registry.json"), nil
}

func (r *Registry) loadRegistryFile() registryJSON {
	path, err := r.registryPath()
	if err != nil {
		return registryJSON{Plugins: make(map[string]registryEntry)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return registryJSON{Plugins: make(map[string]registryEntry)}
	}
	var reg registryJSON
	if err := json.Unmarshal(data, &reg); err != nil {
		return registryJSON{Plugins: make(map[string]registryEntry)}
	}
	if reg.Plugins == nil {
		reg.Plugins = make(map[string]registryEntry)
	}
	return reg
}

func (r *Registry) saveRegistryFile() {
	path, err := r.registryPath()
	if err != nil {
		return
	}
	reg := registryJSON{Plugins: make(map[string]registryEntry)}
	for name, p := range r.plugins {
		reg.Plugins[name] = registryEntry{
			Version:     p.Version,
			Source:      p.Source,
			Enabled:     p.Enabled,
			InstalledAt: p.InstalledAt.Format(time.RFC3339),
		}
	}
	data, _ := json.MarshalIndent(reg, "", "  ")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, data, 0o644)
}

func (r *Registry) gitClone(url, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return runGit(dest, "pull", "--ff-only")
	}
	return runGit("", "clone", "--depth", "1", url, dest)
}

func repoName(url string) string {
	// Extract owner-repo from URL.
	s := strings.TrimSuffix(url, ".git")
	s = strings.TrimSuffix(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "-" + parts[len(parts)-1]
	}
	return s
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
