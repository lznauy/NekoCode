package plugin

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"nekocode/util/fs"
)

// registry manages plugin lifecycle.
type registry struct {
	mu        sync.RWMutex
	installMu sync.Mutex
	plugins   map[string]*Plugin
	baseDirs  []string

	Logf func(string, ...any)
}

// DefaultDirs returns plugin search paths (project > user).
func defaultDirs() []string {
	return fs.NekocodeDirs("plugins")
}

// newRegistry creates a plugin registry scanning baseDirs.
func newRegistry(baseDirs []string) *registry {
	return &registry{
		plugins:  make(map[string]*Plugin),
		baseDirs: baseDirs,
	}
}

// LoadAll scans all base dirs and loads plugin manifests.
func (r *registry) LoadAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	regData := r.loadRegistryFile()
	r.plugins = make(map[string]*Plugin)
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
			if !hasManifest(pluginDir) {
				continue
			}
			r.loadPlugin(pluginDir, regData, seen)
		}
	}
	if err := r.saveRegistryFile(); err != nil && r.Logf != nil {
		r.Logf("plugin: migrate registry: %v", err)
	}
}

func (r *registry) loadPlugin(pluginDir string, regData registryJSON, seen map[string]bool) *Plugin {
	m, err := parseManifest(pluginDir)
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
		source = sanitizeSource(re.Source)
		if t, err := time.Parse(time.RFC3339, re.InstalledAt); err == nil {
			installedAt = t
		}
	}

	p := newPluginFromManifest(m, pluginDir, source)
	p.Enabled = enabled
	p.InstalledAt = installedAt
	r.plugins[m.Name] = p
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
func (r *registry) Uninstall(name string) error {
	r.installMu.Lock()
	defer r.installMu.Unlock()

	r.mu.Lock()
	p, ok := r.plugins[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("plugin %q not found", name)
	}
	backupDir, err := os.MkdirTemp(filepath.Dir(p.Dir), ".uninstall-")
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("create uninstall backup: %w", err)
	}
	if err := os.Remove(backupDir); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("prepare uninstall backup: %w", err)
	}
	if err := os.Rename(p.Dir, backupDir); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("backup plugin dir: %w", err)
	}
	delete(r.plugins, name)
	if err := r.saveRegistryFile(); err != nil {
		r.plugins[name] = p
		restoreErr := os.Rename(backupDir, p.Dir)
		r.mu.Unlock()
		if restoreErr != nil {
			return fmt.Errorf("save registry: %w (restore plugin: %v)", err, restoreErr)
		}
		return fmt.Errorf("save registry: %w", err)
	}
	r.mu.Unlock()
	if err := os.RemoveAll(backupDir); err != nil && r.Logf != nil {
		r.Logf("plugin: remove uninstall backup %s: %v", backupDir, err)
	}
	return nil
}

// List returns all installed plugins sorted by name.
func (r *registry) List() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.SortedFunc(maps.Values(r.plugins), func(a, b *Plugin) int {
		return strings.Compare(a.Name, b.Name)
	})
}

// Copies returns value copies of all installed plugins sorted by name.
// Callers that publish a snapshot outside the registry lock (management
// views) must use this: Enable/Disable mutate Enabled in place, and the
// registry's RWLock is the only synchronization for those fields.
func (r *registry) Copies() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		snapshot := *p
		out = append(out, &snapshot)
	}
	slices.SortFunc(out, func(a, b *Plugin) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// Get returns a plugin by name.
func (r *registry) Get(name string) (*Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// Enable enables a plugin.
func (r *registry) Enable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	previous := p.Enabled
	p.Enabled = true
	if err := r.saveRegistryFile(); err != nil {
		p.Enabled = previous
		return err
	}
	return nil
}

// Disable disables a plugin without removing it.
func (r *registry) Disable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	previous := p.Enabled
	p.Enabled = false
	if err := r.saveRegistryFile(); err != nil {
		p.Enabled = previous
		return err
	}
	return nil
}

// SkillDirs returns all skill directories from enabled plugins.
func (r *registry) SkillDirs() []string {
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

func userPluginDir() (string, error) {
	return filepath.Join(fs.NekocodeHome(), "plugins"), nil
}

func (r *registry) registryPath() (string, error) {
	dir, err := userPluginDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "registry.json"), nil
}

func (r *registry) loadRegistryFile() registryJSON {
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

func (r *registry) saveRegistryFile() error {
	path, err := r.registryPath()
	if err != nil {
		return err
	}
	reg := registryJSON{Plugins: make(map[string]registryEntry)}
	for name, p := range r.plugins {
		reg.Plugins[name] = registryEntry{
			Version:     p.Version,
			Source:      sanitizeSource(p.Source),
			Enabled:     p.Enabled,
			InstalledAt: p.InstalledAt.Format(time.RFC3339),
		}
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".registry-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
