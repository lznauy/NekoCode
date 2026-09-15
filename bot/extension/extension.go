// Package extension composes plugins, skills, hooks, agents, and MCP servers
// into one runtime owned by the bot.
package extension

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/skill"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/capability"
	"nekocode/bot/policy"
	"nekocode/logger"
)

// Manager is the public extension entry point. Child managers remain private
// so extension activation always follows one lifecycle.
type Manager struct {
	mu          sync.Mutex
	ops         sync.Mutex
	skills      *skill.Manager
	plugins     *plugin.Manager
	mcp         *mcp.Manager
	policy      *policy.Policy
	active      map[string]activePlugin
	commands    *command.Handler
	sessionMCP  map[string][]string
	tools       *tools.Registry
	agentErrors map[string]string
}

// Snapshot is the read-only state used by management views.
type Snapshot struct {
	Skills       []*skill.Skill
	LoadedSkills map[string]bool
	Plugins      []*plugin.Plugin
	MCPHealth    map[string]mcp.Health
	AgentErrors  map[string]string
	Agents       []AgentInfo
}

// Config contains the shared dependencies used by extension modules.
type Config struct {
	Context       *ctxmgr.Manager
	Tools         *tools.Registry
	Policy        *policy.Policy
	ContextWindow int
}

type activePlugin struct {
	plugin *plugin.Plugin
	agents []agentprofile.Profile
	mcpIDs []string
}

// New creates the unified extension manager.
func New(config Config) *Manager {
	m := &Manager{
		skills:      skill.New(config.Context, config.Tools, config.ContextWindow),
		plugins:     plugin.New(),
		mcp:         mcp.New(),
		policy:      config.Policy,
		active:      make(map[string]activePlugin),
		sessionMCP:  make(map[string][]string),
		tools:       config.Tools,
		agentErrors: make(map[string]string),
	}
	// MCP tools reach the model through one constant-schema proxy registered
	// once here — adding/removing servers never changes the tool list, which
	// keeps the provider's cached prompt prefix stable.
	if config.Tools != nil {
		config.Tools.Register(&agentCatalogTool{manager: m})
		config.Tools.RegisterWithOptions(capability.New(m.mcp), tools.RegistrationOptions{
			ResolveTarget: capability.ResolveTarget,
		})
	}
	return m
}

// Load discovers plugins, activates enabled extensions, and loads the
// resulting skill set.
func (m *Manager) Load() {
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()

	m.plugins.Load()
	for _, p := range m.plugins.ListPlugins() {
		if p.Enabled {
			_ = m.activateLocked(context.Background(), p)
		}
	}
	m.skills.Load(m.activeSkillDirsLocked())
	m.syncSkillCommandsLocked()
}

// Reload rebuilds plugin runtime state from disk and preserves loaded skills.
func (m *Manager) Reload() {
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deactivateAllLocked()
	m.plugins.Reload()
	for _, p := range m.plugins.ListPlugins() {
		if p.Enabled {
			_ = m.activateLocked(context.Background(), p)
		}
	}
	m.skills.Reload(m.activeSkillDirsLocked())
	m.syncSkillCommandsLocked()
}

// Close stops plugin runtime extensions and every MCP process.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deactivateAllLocked()
	m.mcp.Close()
}

// Snapshot returns all management state under one lock.
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		Skills:       m.skills.List(),
		LoadedSkills: m.skills.LoadedSet(),
		Plugins:      m.plugins.ListPlugins(),
		MCPHealth:    m.mcp.Health(),
		AgentErrors:  m.agentErrorsLocked(),
		Agents:       m.agentInfosLocked(),
	}
}

// Skill returns the compact command representation of one skill.
func (m *Manager) Skill(name string) (skill.Command, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sk, ok := m.skills.Get(name)
	if !ok {
		return skill.Command{}, false
	}
	return skill.Command{Name: sk.Name, Context: skill.FormatForContext(sk)}, true
}

// CommandSkills returns all skills available to command registration.
func (m *Manager) CommandSkills() []command.SkillRegistration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.commandSkillsLocked()
}

func (m *Manager) commandSkillsLocked() []command.SkillRegistration {
	list := m.skills.List()
	out := make([]command.SkillRegistration, 0, len(list))
	for _, sk := range list {
		name := sk.Name
		out = append(out, command.SkillRegistration{
			Name: name,
			Load: func() (string, bool) {
				command, ok := m.Skill(name)
				return command.Context, ok
			},
			MarkLoaded: func() {
				m.MarkSkillLoaded(name)
			},
		})
	}
	return out
}

func (m *Manager) syncSkillCommandsLocked() {
	if m.commands != nil {
		m.commands.RegisterSkills(m.commandSkillsLocked())
	}
}

func (m *Manager) MarkSkillLoaded(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills.MarkLoaded(name)
}

func (m *Manager) ClearLoadedSkills() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills.ClearLoaded()
}

// RefreshSkillList re-renders the prompt's skill list from the registry.
// The list is a pure function of the registry and the context window, so this
// is safe to call at any time: it only changes the cached prefix when a skill
// was actually added or removed.
func (m *Manager) RefreshSkillList() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills.RefreshList()
}

// SetPluginEnabled persists and applies one plugin state transition.
func (m *Manager) SetPluginEnabled(name string, enabled bool) error {
	_, err := m.setPluginEnabled(context.Background(), name, enabled)
	return err
}

func (m *Manager) setPluginEnabled(ctx context.Context, name string, enabled bool) (bool, error) {
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return false, err
	}
	current, ok := m.plugins.Get(name)
	if !ok {
		return false, fmt.Errorf("plugin not found: %s", name)
	}
	if current.Enabled == enabled && (!enabled || m.agentErrors[name] == "") {
		return false, nil
	}
	next, err := m.plugins.SetEnabled(name, enabled)
	if err != nil {
		return false, err
	}
	if enabled {
		if err := m.activateLocked(ctx, next); err != nil {
			_, _ = m.plugins.SetEnabled(name, false)
			return false, err
		}
	} else {
		m.deactivateLocked(name)
	}
	m.skills.Reload(m.activeSkillDirsLocked())
	m.syncSkillCommandsLocked()
	return true, nil
}

// AddMCPServer starts a host-configured MCP server synchronously.
func (m *Manager) AddMCPServer(name string, cfg mcp.ServerConfig) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mcp.Add(context.Background(), "config:"+name, name, cfg)
}

// AddMCPServerBackground registers a host-configured MCP server and starts it
// asynchronously so application startup is not gated on external processes.
func (m *Manager) AddMCPServerBackground(name string, cfg mcp.ServerConfig) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mcp.AddBackground("config:"+name, name, cfg)
}

// ReplaceSessionMCPServers atomically replaces all transport-supplied MCP
// servers owned by source. Host-configured servers win name collisions.
func (m *Manager) ReplaceSessionMCPServers(ctx context.Context, source string, configs map[string]mcp.ServerConfig) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()

	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	registrations := make([]mcp.Registration, 0, len(names))
	ids := make([]string, 0, len(names))
	for _, name := range names {
		if owner := m.mcp.Owner(name); owner != "" && !strings.HasPrefix(owner, "session:") {
			logger.Log("extension: session MCP server %q skipped, host server already registered by %s", name, owner)
			continue
		}
		id := "session:" + source + ":" + name
		registrations = append(registrations, mcp.Registration{ID: id, Name: name, Config: configs[name]})
		ids = append(ids, id)
	}
	if err := m.mcp.Replace(ctx, m.sessionMCP[source], registrations); err != nil {
		return err
	}
	if len(ids) == 0 {
		delete(m.sessionMCP, source)
	} else {
		m.sessionMCP[source] = ids
	}
	return nil
}

func (m *Manager) activateLocked(ctx context.Context, p *plugin.Plugin) error {
	state := activePlugin{plugin: p}
	seen := make(map[string]bool)
	for _, path := range p.AgentPaths() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var hasTool func(string) bool
		if m.tools != nil {
			hasTool = m.tools.Has
		}
		profile, err := agentprofile.Load(p.Dir, path, hasTool)
		if err == nil && seen[profile.Name] {
			err = fmt.Errorf("duplicate agent name %q", profile.Name)
		}
		if err != nil {
			err = fmt.Errorf("agent %s: %w", path, err)
			m.agentErrors[p.Name] = err.Error()
			logger.Log("plugin: %s: %v", p.Name, err)
			return err
		}
		seen[profile.Name] = true
		state.agents = append(state.agents, profile)
	}
	delete(m.agentErrors, p.Name)
	m.active[p.Name] = state

	if err := ctx.Err(); err != nil {
		m.deactivateLocked(p.Name)
		return err
	}
	if hooksPath, ok := p.HooksPath(); ok && m.policy != nil {
		hooks, err := policy.LoadPluginHooks(p.Dir, hooksPath)
		if err != nil {
			logger.Log("plugin: hooks %s: %v", hooksPath, err)
		} else {
			for _, hook := range hooks {
				m.policy.Register(hook)
			}
		}
	}

	for name, cfg := range p.MCPServers() {
		if err := ctx.Err(); err != nil {
			m.deactivateLocked(p.Name)
			return err
		}
		id := "plugin:" + p.Name + ":" + name
		state.mcpIDs = append(state.mcpIDs, id)
		m.active[p.Name] = state
		if err := m.mcp.AddBackground(id, name, plugin.ExpandPluginMCPConfig(cfg, p.Dir)); err != nil {
			logger.Log("plugin: mcp %s: %v", name, err)
			if ctx.Err() != nil {
				m.deactivateLocked(p.Name)
				return ctx.Err()
			}
		}
	}
	m.active[p.Name] = state
	return nil
}

func (m *Manager) deactivateLocked(name string) {
	delete(m.agentErrors, name)
	state, ok := m.active[name]
	if !ok {
		return
	}
	for _, id := range state.mcpIDs {
		m.mcp.Remove(id)
	}
	if m.policy != nil {
		m.policy.UnregisterPrefix("plugin:" + state.plugin.Dir + ":")
	}
	delete(m.active, name)
}

func (m *Manager) deactivateAllLocked() {
	clear(m.agentErrors)
	for name := range m.active {
		m.deactivateLocked(name)
	}
}
