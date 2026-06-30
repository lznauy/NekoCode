package skill

import (
	"fmt"
	"os"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/tools"
	"nekocode/common"
)

type Manager struct {
	reg             *Registry
	ctx             *ctxmgr.Manager
	tools           *tools.Registry
	contextWindow   int
	pluginSkillDirs func() []string
	logf            func(string, ...any)
}

type ManagerOptions struct {
	Context         *ctxmgr.Manager
	Tools           *tools.Registry
	ContextWindow   int
	PluginSkillDirs func() []string
	Logf            func(string, ...any)
}

func NewManager(opts ManagerOptions) *Manager {
	return &Manager{
		ctx:             opts.Context,
		tools:           opts.Tools,
		contextWindow:   opts.ContextWindow,
		pluginSkillDirs: opts.PluginSkillDirs,
		logf:            opts.Logf,
	}
}

func (m *Manager) Init() {
	m.Reload(nil)
}

func (m *Manager) Reload(loaded map[string]bool) {
	m.reg = NewRegistry()
	m.reg.RegisterBundled(BundledSkills())
	dirs := DefaultDirs()
	if m.pluginSkillDirs != nil {
		dirs = append(dirs, m.pluginSkillDirs()...)
	}
	if err := m.reg.Load(dirs); err != nil {
		m.log("skill: load error: %v", err)
	}
	for name := range loaded {
		if m.reg.Has(name) {
			m.reg.MarkLoaded(name)
		}
	}
	m.RefreshList()
	m.RegisterTool()
}

func (m *Manager) ReloadPreservingLoaded() {
	m.Reload(m.LoadedSet())
}

func (m *Manager) LoadPluginSkillDirs(dirs []string) {
	if m == nil || m.reg == nil {
		return
	}
	for _, dir := range dirs {
		if err := m.reg.Load([]string{dir}); err != nil {
			m.log("plugin: skill load error: %v", err)
		}
	}
	m.RefreshList()
	m.RegisterTool()
}

func (m *Manager) Registry() *Registry {
	if m == nil {
		return nil
	}
	return m.reg
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

func (m *Manager) MarkLoaded(name string) {
	if m == nil || m.reg == nil {
		return
	}
	m.reg.MarkLoaded(name)
	m.RefreshList()
}

func (m *Manager) ClearLoaded() {
	if m == nil || m.reg == nil {
		return
	}
	m.reg.ClearLoaded()
	m.RefreshList()
}

func (m *Manager) RefreshList() {
	if m == nil || m.ctx == nil || m.reg == nil {
		return
	}
	m.ctx.SetSkillList(BuildSkillListText(m.reg.List(), m.reg.LoadedSet(), m.contextWindow))
}

func (m *Manager) RegisterTool() {
	if m == nil || m.tools == nil || m.reg == nil {
		return
	}
	m.tools.Unregister("skill")
	m.tools.Register(NewSkillTool(m.reg))
}

func (m *Manager) ManagementView(plugins []common.PluginView, mcp []common.MCPServerView) common.SkillManagementView {
	return BuildManagementView(m.Registry(), plugins, mcp)
}

func (m *Manager) log(format string, args ...any) {
	if m.logf != nil {
		m.logf(format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
