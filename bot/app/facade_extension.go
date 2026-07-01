package app

import (
	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/common/debug"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/skill"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/common"
)

type extensionFacade struct {
	skills     *skill.Manager
	plugins    *plugin.Manager
	mcpClients map[string]*mcp.Client
	mcpHealth  map[string]mcpHealth
	configMCP  []common.MCPServerView

	ctxMgr        *ctxmgr.Manager
	toolRegistry  *tools.Registry
	hookReg       *hooks.Registry
	contextWindow int
}

type mcpHealth struct {
	Status    string
	Error     string
	ToolCount int
}

func newExtensionFacade(ctxMgr *ctxmgr.Manager, toolRegistry *tools.Registry, hookReg *hooks.Registry, contextWindow int) *extensionFacade {
	return &extensionFacade{
		ctxMgr:        ctxMgr,
		toolRegistry:  toolRegistry,
		hookReg:       hookReg,
		contextWindow: contextWindow,
		mcpClients:    make(map[string]*mcp.Client),
		mcpHealth:     make(map[string]mcpHealth),
	}
}

func (e *extensionFacade) InitSkills() {
	e.skills = skill.NewManager(skill.ManagerOptions{
		Context:       e.ctxMgr,
		Tools:         e.toolRegistry,
		ContextWindow: e.contextWindow,
		PluginSkillDirs: func() []string {
			if e.plugins == nil {
				return nil
			}
			return e.plugins.SkillDirs()
		},
		Logf: debug.Log,
	})
	e.skills.Init()
}

func (e *extensionFacade) ReloadSkills() {
	if e.skills != nil {
		e.skills.ReloadPreservingLoaded()
	}
}

func (e *extensionFacade) RefreshPluginSkills() {
	e.ReloadSkills()
}

func (e *extensionFacade) RefreshSkillList() {
	if e.skills != nil {
		e.skills.RefreshList()
	}
}

func (e *extensionFacade) SkillManagementView() common.SkillManagementView {
	mcpServers := e.plugins.MCPServers()
	mcpServers = append(mcpServers, e.configMCP...)
	e.applyMCPHealth(mcpServers)
	return e.skills.ManagementView(e.plugins.Views(), mcpServers)
}

func (e *extensionFacade) applyMCPHealth(servers []common.MCPServerView) {
	for i := range servers {
		if !servers[i].PluginEnabled {
			servers[i].Status = "disabled"
			continue
		}
		health, ok := e.mcpHealth[servers[i].Name]
		if !ok {
			servers[i].Status = "unknown"
			continue
		}
		servers[i].Status = health.Status
		servers[i].Error = health.Error
		servers[i].ToolCount = health.ToolCount
	}
}

func (e *extensionFacade) SetPluginEnabled(name string, enabled bool) (common.SkillManagementView, error) {
	if _, err := e.plugins.SetEnabled(name, enabled); err != nil {
		return common.SkillManagementView{}, err
	}
	return e.SkillManagementView(), nil
}

func (e *extensionFacade) RefreshSkillManagement() common.SkillManagementView {
	e.plugins.Reload()
	return e.SkillManagementView()
}

type skillCommandProvider struct {
	manager *skill.Manager
}

func (p skillCommandProvider) ListForCommands() []command.SkillCommand {
	if p.manager == nil {
		return nil
	}
	skills := p.manager.List()
	out := make([]command.SkillCommand, 0, len(skills))
	for _, sk := range skills {
		out = append(out, command.SkillCommand{
			Name:    sk.Name,
			Context: skill.FormatForContext(sk),
		})
	}
	return out
}

func (p skillCommandProvider) GetForCommand(name string) (command.SkillCommand, bool) {
	if p.manager == nil {
		return command.SkillCommand{}, false
	}
	sk, ok := p.manager.Get(name)
	if !ok {
		return command.SkillCommand{}, false
	}
	return command.SkillCommand{
		Name:    sk.Name,
		Context: skill.FormatForContext(sk),
	}, true
}

func (p skillCommandProvider) MarkLoaded(name string) {
	if p.manager != nil {
		p.manager.MarkLoaded(name)
	}
}

func (p skillCommandProvider) ClearLoaded() {
	if p.manager != nil {
		p.manager.ClearLoaded()
	}
}
