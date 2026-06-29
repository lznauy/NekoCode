package app

import (
	"fmt"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/debug"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/hooks"
	"nekocode/bot/plugin"
	"nekocode/bot/skill"
	botskill "nekocode/bot/skill"
	"nekocode/bot/tools"
)

type extensionFacade struct {
	skills     *skill.Manager
	plugins    *plugin.Manager
	mcpClients map[string]*mcp.Client

	ctxMgr        *ctxmgr.Manager
	toolRegistry  *tools.Registry
	hookReg       *hooks.Registry
	contextWindow int
	cmdParser     *command.Parser
	cb            *callbackBus
}

type extensionDeps struct {
	CtxMgr        *ctxmgr.Manager
	ToolRegistry  *tools.Registry
	HookReg       *hooks.Registry
	ContextWindow int
	CmdParser     *command.Parser
	Callbacks     *callbackBus
}

func (e *extensionFacade) Init(d extensionDeps) {
	e.ctxMgr = d.CtxMgr
	e.toolRegistry = d.ToolRegistry
	e.hookReg = d.HookReg
	e.contextWindow = d.ContextWindow
	e.cmdParser = d.CmdParser
	e.cb = d.Callbacks
	e.mcpClients = make(map[string]*mcp.Client)
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

func (e *extensionFacade) InitPlugins() {
	e.closePluginMCPServers()
	e.resetPluginMCPClients()
	e.plugins = plugin.NewManager(plugin.ManagerOptions{
		Hooks: e.hookReg,
		Logf:  debug.Log,
		OnInstall: func(p *plugin.Plugin) {
			if e.skills != nil {
				e.skills.LoadPluginSkillDirs(p.SkillDirs())
			}
		},
		OnChanged:           e.RefreshPluginSkills,
		RegisterAgentPath:   e.registerPluginAgentPath,
		UnregisterAgentPath: e.unregisterPluginAgentPath,
		RegisterMCPServer:   e.registerPluginMCPServer,
		UnregisterMCPServer: e.unregisterPluginMCPServer,
	})
	e.plugins.LoadAll()
}

func (e *extensionFacade) RegisterPluginCommands(p *command.Parser) {
	p.Register("plugin", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) == 0 {
			return plugin.Usage(), true
		}
		switch cmd.Args[0] {
		case "install":
			return e.plugins.Install(cmd.Args[1:], e.cb.InstallCallbacks()), true
		case "uninstall":
			return e.plugins.Uninstall(cmd.Args[1:]), true
		case "list":
			return e.plugins.List(cmd.Args[1:]), true
		case "enable":
			return e.plugins.Enable(cmd.Args[1:]), true
		case "disable":
			return e.plugins.Disable(cmd.Args[1:]), true
		case "info":
			return e.plugins.Info(cmd.Args[1:]), true
		default:
			return fmt.Sprintf("Unknown subcommand: %s\n%s", cmd.Args[0], plugin.Usage()), true
		}
	})
}

func (e *extensionFacade) registerPluginAgentPath(path string) error {
	def, err := subagent.ParseAgentMD(path)
	if err != nil {
		return err
	}
	subagent.RegisterPlugin(def.ToAgentType())
	return nil
}

func (e *extensionFacade) unregisterPluginAgentPath(path string) {
	def, err := subagent.ParseAgentMD(path)
	if err == nil {
		subagent.UnregisterPlugin(def.Name)
	}
}

func (e *extensionFacade) resetPluginMCPClients() {
	e.mcpClients = make(map[string]*mcp.Client)
}

func (e *extensionFacade) closePluginMCPServers() {
	for name, client := range e.mcpClients {
		client.Close()
		delete(e.mcpClients, name)
	}
}

func (e *extensionFacade) registerPluginMCPServer(pluginDir, name string, cfg plugin.MCPServerConfig) error {
	level := mcp.ParseDangerLevel(cfg.DangerLevel)
	cfg.Env = plugin.ExpandPluginEnv(cfg.Env, pluginDir)
	client := mcp.NewClient(name, cfg)
	if old, exists := e.mcpClients[name]; exists {
		old.Close()
	}
	e.mcpClients[name] = client

	if err := client.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	mcpTools, err := client.ListTools()
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	for _, td := range mcpTools {
		e.toolRegistry.Register(mcp.NewMCPTool(client, td, level))
	}
	return nil
}

func (e *extensionFacade) unregisterPluginMCPServer(name string) {
	client, ok := e.mcpClients[name]
	if !ok {
		return
	}
	for _, t := range e.toolRegistry.List() {
		if plugin.IsMCPToolForClient(t.Name(), client.Name) {
			e.toolRegistry.Unregister(t.Name())
		}
	}
	client.Close()
	delete(e.mcpClients, name)
}

func (e *extensionFacade) SkillManagementSnapshot() botskill.ManagementSnapshot {
	return e.skills.ManagementSnapshot(skillPluginSnapshots(e.plugins.Snapshots()), skillMCPSnapshots(e.plugins.MCPServers()))
}

func (e *extensionFacade) SetPluginEnabled(name string, enabled bool) (botskill.ManagementSnapshot, error) {
	if _, err := e.plugins.SetEnabled(name, enabled); err != nil {
		return botskill.ManagementSnapshot{}, err
	}
	return e.SkillManagementSnapshot(), nil
}

func (e *extensionFacade) RefreshSkillManagement() botskill.ManagementSnapshot {
	e.plugins.Reload()
	return e.SkillManagementSnapshot()
}
