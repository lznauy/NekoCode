// Package skill provides a file-based skill system: skills are discovered
// from SKILL.md files (built-in, plugin, and user directories), exposed to
// the model through a "skill" tool, and injected into the conversation
// context on demand.
//
// Manager is the package entry point; the skill registry and the tool
// adapter are internal implementation details.
package skill

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
)

// Skill represents a loaded skill definition: the parsed content of one
// SKILL.md file plus its auxiliary files.
type Skill struct {
	Name        string
	Description string
	Content     string
	Dir         string
	Files       []string

	Context                string
	AgentProfile           string
	AllowedTools           []string
	MaxSteps               int
	ContextWindow          int
	DisableModelInvocation bool
}

// Command is the compact skill representation consumed by command handlers.
type Command struct {
	Name    string
	Context string
}

// Manager owns skill discovery, loaded-state tracking, context injection,
// and registration of the "skill" tool.
type Manager struct {
	reg           *registry
	ctx           *ctxmgr.Manager
	tools         *tools.Registry
	contextWindow int
}

// New creates a skill manager with the concrete services it owns.
func New(ctx *ctxmgr.Manager, toolRegistry *tools.Registry, contextWindow int) *Manager {
	m := &Manager{
		reg:           newRegistry(),
		ctx:           ctx,
		tools:         toolRegistry,
		contextWindow: contextWindow,
	}
	// Register the capability before extensions validate Agent tools. Its
	// catalog remains empty until Load publishes successfully activated skills.
	m.RegisterTool()
	return m
}

// Load discovers built-in, local, and plugin skills.
func (m *Manager) Load(pluginDirs []string) {
	m.reload(pluginDirs, nil)
}

// Reload repeats discovery while preserving loaded skill state.
func (m *Manager) Reload(pluginDirs []string) {
	m.reload(pluginDirs, m.LoadedSet())
}

func (m *Manager) reload(pluginDirs []string, loaded map[string]bool) {
	m.reg = newRegistry()
	m.reg.RegisterAll(bundledSkills())
	dirs := append(defaultDirs(), pluginDirs...)
	m.reg.Load(dirs)
	for name := range loaded {
		if m.reg.Has(name) {
			m.reg.MarkLoaded(name)
		}
	}
	m.RefreshList()
	m.RegisterTool()
}

func (m *Manager) List() []*Skill {
	if m == nil || m.reg == nil {
		return nil
	}
	return m.reg.List()
}

func (m *Manager) Get(name string) (*Skill, bool) {
	if m == nil || m.reg == nil {
		return nil, false
	}
	return m.reg.Get(name)
}

func (m *Manager) LoadedSet() map[string]bool {
	if m == nil || m.reg == nil {
		return nil
	}
	return m.reg.LoadedSet()
}

// MarkLoaded records that a skill's content has been injected into the
// conversation. It no longer touches the prompt's skill list: loaded state is
// per-session bookkeeping used by the skill tool (so a reload after context
// compaction still works) and by session persistence. The list itself stays a
// pure function of the registry, which keeps the cached prefix byte-identical
// across a session restore.
func (m *Manager) MarkLoaded(name string) {
	if m == nil || m.reg == nil {
		return
	}
	m.reg.MarkLoaded(name)
}

func (m *Manager) ClearLoaded() {
	if m == nil || m.reg == nil {
		return
	}
	m.reg.ClearLoaded()
}

// RefreshList re-renders the prompt's skill list. It is idempotent: the list
// depends only on the registry and the context window, so calling it at any
// point cannot invalidate a cached prefix unless the registry really changed.
func (m *Manager) RefreshList() {
	if m == nil || m.ctx == nil || m.reg == nil {
		return
	}
	m.ctx.SetSkillList(buildSkillListText(m.reg.List(), m.contextWindow))
}

func (m *Manager) RegisterTool() {
	if m == nil || m.tools == nil || m.reg == nil {
		return
	}
	m.tools.Unregister("skill")
	m.tools.Register(newSkillTool(m.reg))
}
