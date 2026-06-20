package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"nekocode/common"
)

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
	return filepath.Join(common.NekocodeHome(), "plugins"), nil
}

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
