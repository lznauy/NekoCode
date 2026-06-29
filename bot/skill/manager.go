package skill

import (
	"fmt"
	"os"

	ctxmgr "nekocode/bot/contextmgr"
	extskill "nekocode/bot/extension/skill"
	extbundled "nekocode/bot/extension/skill/bundled"
	"nekocode/bot/skillview"
	"nekocode/bot/tools"
)

type Manager struct {
	reg             *extskill.Registry
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
	m.reg = extskill.NewRegistry()
	m.reg.RegisterBundled(extbundled.All())
	dirs := extskill.DefaultDirs()
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

func (m *Manager) Registry() *extskill.Registry {
	if m == nil {
		return nil
	}
	return m.reg
}

func (m *Manager) List() []*extskill.Skill {
	if m == nil || m.reg == nil {
		return nil
	}
	return m.reg.List()
}

func (m *Manager) Get(name string) (*extskill.Skill, bool) {
	if m == nil || m.reg == nil {
		return nil, false
	}
	return m.reg.Get(name)
}

func (m *Manager) ListForCommands() []skillview.Skill {
	if m == nil || m.reg == nil {
		return nil
	}
	skills := m.reg.List()
	out := make([]skillview.Skill, 0, len(skills))
	for _, sk := range skills {
		out = append(out, skillview.Skill{
			Name:    sk.Name,
			Context: extskill.FormatForContext(sk),
		})
	}
	return out
}

func (m *Manager) GetForCommand(name string) (skillview.Skill, bool) {
	sk, ok := m.Get(name)
	if !ok {
		return skillview.Skill{}, false
	}
	return skillview.Skill{
		Name:    sk.Name,
		Context: extskill.FormatForContext(sk),
	}, true
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
	m.ctx.SetSkillList(extskill.BuildSkillListText(m.reg.List(), m.reg.LoadedSet(), m.contextWindow))
}

func (m *Manager) RegisterTool() {
	if m == nil || m.tools == nil || m.reg == nil {
		return
	}
	m.tools.Unregister("skill")
	m.tools.Register(extskill.NewSkillTool(m.reg))
}

func (m *Manager) ManagementSnapshot(plugins []PluginSnapshot, mcp []MCPServerSnapshot) ManagementSnapshot {
	return BuildManagementSnapshot(m.Registry(), plugins, mcp)
}

func (m *Manager) log(format string, args ...any) {
	if m.logf != nil {
		m.logf(format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
