package plugin

import (
	"fmt"
	"strings"

	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/common"
)

const InstallUsage = "Usage: /plugin install <source>\n  source: GitHub URL | user/repo | ./local-path"

type Manager struct {
	reg       *Registry
	runtime   runtime
	onInstall func(*Plugin)
	onChanged func()
}

type ManagerOptions struct {
	Hooks               *hooks.Registry
	Logf                func(string, ...any)
	OnInstall           func(*Plugin)
	OnChanged           func()
	RegisterAgentPath   func(path string) error
	UnregisterAgentPath func(path string)
	RegisterMCPServer   func(pluginDir, name string, cfg MCPServerConfig) error
	UnregisterMCPServer func(name string)
}

type InstallCallbacks struct {
	Confirm    func(source string, p *Plugin, isRemote bool) bool
	Notify     func(string)
	SetPending func(bool)
	Unblock    func()
}

type installArgs struct {
	Source    string
	Confirmed bool
	OK        bool
}

type lookupResult struct {
	Plugin  *Plugin
	Message string
	OK      bool
}

type runtime struct {
	Hooks               *hooks.Registry
	Logf                func(string, ...any)
	RegisterAgentPath   func(path string) error
	UnregisterAgentPath func(path string)
	RegisterMCPServer   func(pluginDir, name string, cfg MCPServerConfig) error
	UnregisterMCPServer func(name string)
}

func NewManager(opts ManagerOptions) *Manager {
	reg := NewRegistry(DefaultDirs())
	reg.Logf = opts.Logf
	if reg.Logf == nil {
		reg.Logf = debug.Log
	}
	return &Manager{
		reg:       reg,
		onInstall: opts.OnInstall,
		onChanged: opts.OnChanged,
		runtime: runtime{
			Hooks:               opts.Hooks,
			Logf:                reg.Logf,
			RegisterAgentPath:   opts.RegisterAgentPath,
			UnregisterAgentPath: opts.UnregisterAgentPath,
			RegisterMCPServer:   opts.RegisterMCPServer,
			UnregisterMCPServer: opts.UnregisterMCPServer,
		},
	}
}

func (m *Manager) Registry() *Registry {
	return m.reg
}

func (m *Manager) LoadAll() {
	m.reg.LoadAll()
	for _, p := range m.reg.List() {
		if p.Enabled {
			m.loadExtensions(p)
		}
	}
}

func (m *Manager) Reload() {
	m.reg.LoadAll()
	if m.onChanged != nil {
		m.onChanged()
	}
}

func (m *Manager) ListPlugins() []*Plugin {
	return m.reg.List()
}

func (m *Manager) SkillDirs() []string {
	return m.reg.SkillDirs()
}

// MCPServers returns flattened MCP server views for all installed plugins,
// attributed to their source plugin and its current enabled state.
func (m *Manager) MCPServers() []common.MCPServerView {
	plugins := m.reg.List()
	out := make([]common.MCPServerView, 0)
	for _, p := range plugins {
		out = append(out, MCPServersFor(p)...)
	}
	return out
}

func (m *Manager) Get(name string) (*Plugin, bool) {
	return m.reg.Get(name)
}

func (m *Manager) Install(args []string, cb InstallCallbacks) string {
	parsed := parseInstallArgs(args)
	if !parsed.OK {
		return InstallUsage
	}
	source := parsed.Source

	if IsLocalPath(source) {
		return m.installLocal(source, parsed.Confirmed, cb)
	}
	if !parsed.Confirmed {
		if cb.SetPending != nil {
			cb.SetPending(true)
		}
		go m.fetchAndConfirmRemote(source, cb)
		return fmt.Sprintf("Fetching plugin info from %s ...", source)
	}

	go m.installAsync(source, cb)
	return fmt.Sprintf("Installing from %s ...", source)
}

func (m *Manager) Uninstall(args []string) string {
	if len(args) == 0 {
		return "Usage: /plugin uninstall <name>"
	}
	name := args[0]
	if p, ok := m.reg.Get(name); ok {
		m.unloadExtensions(p)
	}
	if err := m.reg.Uninstall(name); err != nil {
		return uninstallFailed(err)
	}
	m.notifyChanged()
	return uninstalled(name)
}

func (m *Manager) List(args []string) string {
	return FormatList(m.reg.List())
}

func (m *Manager) Enable(args []string) string {
	lookup := requirePlugin(args, m.reg.Get, "Usage: /plugin enable <name>")
	if !lookup.OK {
		return lookup.Message
	}
	p := lookup.Plugin
	if p.Enabled {
		return alreadyEnabled(p.Name)
	}
	if err := m.reg.Enable(p.Name); err != nil {
		return enableFailed(err)
	}
	if next, ok := m.reg.Get(p.Name); ok {
		m.loadExtensions(next)
	}
	m.notifyChanged()
	return enabled(p.Name)
}

func (m *Manager) Disable(args []string) string {
	lookup := requirePlugin(args, m.reg.Get, "Usage: /plugin disable <name>")
	if !lookup.OK {
		return lookup.Message
	}
	p := lookup.Plugin
	if !p.Enabled {
		return alreadyDisabled(p.Name)
	}
	m.unloadExtensions(p)
	if err := m.reg.Disable(p.Name); err != nil {
		return disableFailed(err)
	}
	m.notifyChanged()
	return disabled(p.Name)
}

func (m *Manager) Info(args []string) string {
	lookup := requirePlugin(args, m.reg.Get, "Usage: /plugin info <name>")
	if !lookup.OK {
		return lookup.Message
	}
	return FormatInfo(lookup.Plugin)
}

func (m *Manager) SetEnabled(name string, enable bool) (*Plugin, error) {
	p, ok := m.reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}
	if enable {
		if err := m.reg.Enable(p.Name); err != nil {
			return nil, err
		}
		next, _ := m.reg.Get(p.Name)
		if next != nil {
			m.loadExtensions(next)
			p = next
		}
	} else {
		if err := m.reg.Disable(p.Name); err != nil {
			return nil, err
		}
		m.unloadExtensions(p)
	}
	m.notifyChanged()
	return p, nil
}

func (m *Manager) installLocal(source string, confirmed bool, cb InstallCallbacks) string {
	p, err := m.reg.PreviewFromPath(source)
	if err != nil {
		return fmt.Sprintf("Preview failed: %v", err)
	}
	if confirmed {
		result := m.installSync(source)
		return result
	}

	if cb.SetPending != nil {
		cb.SetPending(true)
	}
	go func() {
		if cb.Confirm != nil && cb.Confirm(source, p, false) {
			result := m.installSync(source)
			if cb.Notify != nil {
				cb.Notify(result)
			}
		}
	}()
	return FormatInstallPreview(p)
}

func (m *Manager) fetchAndConfirmRemote(source string, cb InstallCallbacks) {
	p, err := FetchRemotePreview(source, FetchURL)
	if err != nil {
		if cb.Notify != nil {
			cb.Notify(fmt.Sprintf("%v\n\n/plugin install %s --yes  to skip preview.", err, source))
		}
		if cb.Unblock != nil {
			cb.Unblock()
		}
		return
	}
	if cb.Confirm != nil && cb.Confirm(source, p, true) {
		m.installAsync(source, cb)
	}
}

func (m *Manager) installSync(source string) string {
	p, err := m.reg.Install(source)
	if err != nil {
		return installFailed(err)
	}
	return m.registerExtensions(p)
}

func (m *Manager) installAsync(source string, cb InstallCallbacks) {
	p, err := m.reg.Install(source)
	if err != nil {
		if cb.Notify != nil {
			cb.Notify(installFailed(err))
		}
		return
	}
	result := m.registerExtensions(p)
	if cb.Notify != nil {
		cb.Notify(result)
	}
}

func (m *Manager) registerExtensions(p *Plugin) string {
	if m.onInstall != nil {
		m.onInstall(p)
	}
	m.loadExtensions(p)
	m.notifyChanged()
	return FormatInstallResult(p)
}

func (m *Manager) notifyChanged() {
	if m.onChanged != nil {
		m.onChanged()
	}
}

func (m *Manager) loadExtensions(p *Plugin) {
	m.runtime.Load(p)
}

func (m *Manager) unloadExtensions(p *Plugin) {
	m.runtime.Unload(p)
}

func parseInstallArgs(args []string) installArgs {
	if len(args) == 0 {
		return installArgs{}
	}
	return installArgs{
		Source:    args[0],
		Confirmed: len(args) >= 2 && args[1] == "--yes",
		OK:        true,
	}
}

func FetchRemotePreview(source string, fetch func(string) ([]byte, error)) (*Plugin, error) {
	rawURL := SourceToRawURL(source)
	if rawURL == "" {
		return nil, fmt.Errorf("fetch plugin info: preview URL not available for %s", source)
	}
	data, err := fetch(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch plugin info: %w", err)
	}
	m, err := ParseManifestData(data)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}
	return &Plugin{Manifest: *m, Dir: "", Source: source}, nil
}

func ConfirmSummary(p *Plugin, isRemote bool) string {
	summary := FormatInstallPreview(p)
	if isRemote {
		summary += "\n(install.sh will not be executed automatically)"
	}
	return summary
}

func requirePlugin(args []string, lookup func(string) (*Plugin, bool), usage string) lookupResult {
	if len(args) == 0 {
		return lookupResult{Message: usage}
	}
	p, ok := lookup(args[0])
	if !ok {
		return lookupResult{Message: fmt.Sprintf("Plugin %q not found.", args[0])}
	}
	return lookupResult{Plugin: p, OK: true}
}

func alreadyEnabled(name string) string  { return fmt.Sprintf("Plugin %q is already enabled.", name) }
func alreadyDisabled(name string) string { return fmt.Sprintf("Plugin %q is already disabled.", name) }
func enabled(name string) string         { return fmt.Sprintf("Enabled plugin %q.", name) }
func disabled(name string) string        { return fmt.Sprintf("Disabled plugin %q.", name) }
func uninstalled(name string) string     { return fmt.Sprintf("Uninstalled plugin %q.", name) }
func installFailed(err error) string     { return fmt.Sprintf("Install failed: %v", err) }
func uninstallFailed(err error) string   { return fmt.Sprintf("Uninstall failed: %v", err) }
func enableFailed(err error) string      { return fmt.Sprintf("Enable failed: %v", err) }
func disableFailed(err error) string     { return fmt.Sprintf("Disable failed: %v", err) }

func (r runtime) Load(p *Plugin) {
	r.registerAgents(p)
	r.registerHooks(p)
	r.registerMCP(p)
}

func (r runtime) Unload(p *Plugin) {
	r.unregisterAgents(p)
	r.unregisterMCP(p)
	r.unregisterHooks(p)
}

func (r runtime) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
		return
	}
	debug.Log(format, args...)
}

func (r runtime) registerAgents(p *Plugin) {
	if r.RegisterAgentPath == nil {
		return
	}
	for _, agentPath := range p.AgentPaths() {
		if err := r.RegisterAgentPath(agentPath); err != nil {
			r.logf("plugin: agent %s: %v", agentPath, err)
		}
	}
}

func (r runtime) unregisterAgents(p *Plugin) {
	if r.UnregisterAgentPath == nil {
		return
	}
	for _, ap := range p.AgentPaths() {
		r.UnregisterAgentPath(ap)
	}
}

func (r runtime) registerHooks(p *Plugin) {
	if r.Hooks == nil {
		return
	}
	if hooksPath, ok := p.HooksPath(); ok {
		if pluginHooks, err := hooks.LoadPluginHooks(p.Dir, hooksPath); err == nil {
			for _, h := range pluginHooks {
				r.Hooks.Register(h)
			}
		} else {
			r.logf("plugin: hooks %s: %v", hooksPath, err)
		}
	}
}

func (r runtime) unregisterHooks(p *Plugin) {
	if r.Hooks == nil {
		return
	}
	r.Hooks.UnregisterWhere(func(h hooks.Hook) bool {
		return strings.HasPrefix(h.Name, "plugin:") && strings.Contains(h.Name, p.Dir)
	})
}

func (r runtime) registerMCP(p *Plugin) {
	if r.RegisterMCPServer == nil {
		return
	}
	for name, cfg := range p.MCPServers() {
		if err := r.RegisterMCPServer(p.Dir, name, cfg); err != nil {
			r.logf("plugin: mcp %s: %v", name, err)
		}
	}
}

func (r runtime) unregisterMCP(p *Plugin) {
	if r.UnregisterMCPServer == nil {
		return
	}
	for srvName := range p.MCPServers() {
		r.UnregisterMCPServer(srvName)
	}
}

func IsMCPToolForClient(toolName, clientName string) bool {
	return strings.HasPrefix(toolName, clientName+"__")
}
