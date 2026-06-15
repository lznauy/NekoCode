package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/mcp"
)

// Author from plugin.json.
type Author struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// CommandEntry from plugin.json.
type CommandEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
}

// HookEntry from plugin.json.
type HookEntry struct {
	Source string `json:"source"`
}

// MCPServerConfig from plugin.json mcpServers map.
// Alias for mcp.ServerConfig to avoid duplicating the struct definition.
type MCPServerConfig = mcp.ServerConfig

// Manifest maps .claude-plugin/plugin.json.
type Manifest struct {
	Name        string                     `json:"name"`
	Version     string                     `json:"version,omitempty"`
	Description string                     `json:"description,omitempty"`
	Author      *Author                    `json:"author,omitempty"`
	Repository  string                     `json:"repository,omitempty"`
	License     string                     `json:"license,omitempty"`
	Keywords    []string                   `json:"keywords,omitempty"`
	Skills      []string                   `json:"skills,omitempty"`
	Agents      []string                   `json:"agents,omitempty"`
	Commands    []CommandEntry             `json:"commands,omitempty"`
	Hooks       *HookEntry                 `json:"hooks,omitempty"`
	MCPServers  map[string]MCPServerConfig `json:"mcpServers,omitempty"`
}

// ParseManifest reads .claude-plugin/plugin.json from a plugin directory.
func ParseManifest(pluginDir string) (*Manifest, error) {
	path := filepath.Join(pluginDir, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return ParseManifestData(data)
}

// ParseManifestData parses manifest JSON from raw bytes.
func ParseManifestData(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("missing required field: name")
	}
	return &m, nil
}

// HasManifest checks if a directory contains .claude-plugin/plugin.json.
func HasManifest(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil
}
