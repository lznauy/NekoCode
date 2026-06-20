package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"nekocode/common"
)

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

// NewRegistry creates a plugin registry scanning baseDirs.
func NewRegistry(baseDirs []string) *Registry {
	return &Registry{
		plugins:  make(map[string]*Plugin),
		baseDirs: baseDirs,
	}
}

// LoadAll scans all base dirs and loads plugin manifests.
func (r *Registry) LoadAll() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var skillDirs []string
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
			p := r.loadPlugin(pluginDir, regData, seen)
			if p == nil {
				continue
			}
			if p.Enabled {
				skillDirs = append(skillDirs, p.SkillDirs()...)
			}
		}
	}

	return skillDirs
}

func (r *Registry) loadPlugin(pluginDir string, regData registryJSON, seen map[string]bool) *Plugin {
	m, err := ParseManifest(pluginDir)
	if err != nil {
		if r.Logf != nil {
			r.Logf("plugin: skip %s: %v", pluginDir, err)
		}
		return nil
	}
	if seen[m.Name] {
		return nil
	}
	seen[m.Name] = true

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

	p := newPluginFromManifest(m, pluginDir, source)
	p.Enabled = enabled
	p.InstalledAt = installedAt
	r.plugins[m.Name] = p

	if r.Logf != nil {
		r.Logf("plugin: loaded %s v%s (enabled=%v) from %s", m.Name, m.Version, enabled, pluginDir)
	}
	return p
}

func newPluginFromManifest(m *Manifest, dir, source string) *Plugin {
	return &Plugin{
		Manifest:       *m,
		Dir:            dir,
		Source:         source,
		Enabled:        true,
		InstalledAt:    time.Now(),
		HasInstallStub: fileExists(filepath.Join(dir, "install.sh")),
	}
}

// Uninstall removes a plugin from disk and registry.
func (r *Registry) Uninstall(name string) error {
	r.mu.RLock()
	p, ok := r.plugins[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

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
