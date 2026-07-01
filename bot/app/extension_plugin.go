package app

import (
	"fmt"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/command"
	"nekocode/common/debug"
	"nekocode/bot/extension/plugin"
)

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

func (e *extensionFacade) RegisterPluginCommands(p *command.Parser, callbacks plugin.InstallCallbacks) {
	p.Register("plugin", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) == 0 {
			return plugin.Usage(), true
		}
		switch cmd.Args[0] {
		case "install":
			return e.plugins.Install(cmd.Args[1:], callbacks), true
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
