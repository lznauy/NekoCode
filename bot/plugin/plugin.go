package plugin

import ext "nekocode/bot/extension/plugin"

type Author = ext.Author
type CommandEntry = ext.CommandEntry
type HookEntry = ext.HookEntry
type MCPServerConfig = ext.MCPServerConfig
type Manifest = ext.Manifest
type Plugin = ext.Plugin
type Registry = ext.Registry

func DefaultDirs() []string { return ext.DefaultDirs() }

func NewRegistry(baseDirs []string) *Registry { return ext.NewRegistry(baseDirs) }

func ParseManifest(pluginDir string) (*Manifest, error) { return ext.ParseManifest(pluginDir) }

func ParseManifestData(data []byte) (*Manifest, error) { return ext.ParseManifestData(data) }

func HasManifest(dir string) bool { return ext.HasManifest(dir) }
