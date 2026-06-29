package app

import (
	"fmt"

	"nekocode/bot/command"
	"nekocode/bot/debug"
	"nekocode/bot/plugin"
	"nekocode/bot/skill"
	"nekocode/common"
)

func (b *Bot) initSkills() {
	b.skills = skill.NewManager(skill.ManagerOptions{
		Context:       b.ctxMgr,
		Tools:         b.toolRegistry,
		ContextWindow: b.cfg.ContextWindow,
		PluginSkillDirs: func() []string {
			if b.plugins == nil {
				return nil
			}
			return b.plugins.SkillDirs()
		},
		Logf: debug.Log,
	})
	b.skills.Init()
}

func (b *Bot) reloadSkills() {
	if b.skills != nil {
		b.skills.ReloadPreservingLoaded()
	}
}

func (b *Bot) initPlugins() {
	b.closePluginMCPServers()
	b.resetPluginMCPClients()
	b.plugins = plugin.NewManager(plugin.ManagerOptions{
		Hooks: b.hookReg,
		Logf:  debug.Log,
		OnInstall: func(p *plugin.Plugin) {
			if b.skills != nil {
				b.skills.LoadPluginSkillDirs(p.SkillDirs())
			}
		},
		OnChanged:           b.refreshPluginSkills,
		RegisterAgentPath:   b.registerPluginAgentPath,
		UnregisterAgentPath: b.unregisterPluginAgentPath,
		RegisterMCPServer:   b.registerPluginMCPServer,
		UnregisterMCPServer: b.unregisterPluginMCPServer,
	})
	b.plugins.LoadAll()
}

func (b *Bot) registerPluginCommands() {
	b.cmdParser.Register("plugin", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) == 0 {
			return plugin.Usage(), true
		}
		switch cmd.Args[0] {
		case "install":
			return b.plugins.Install(cmd.Args[1:], b.pluginInstallCallbacks()), true
		case "uninstall":
			return b.plugins.Uninstall(cmd.Args[1:]), true
		case "list":
			return b.plugins.List(cmd.Args[1:]), true
		case "enable":
			return b.plugins.Enable(cmd.Args[1:]), true
		case "disable":
			return b.plugins.Disable(cmd.Args[1:]), true
		case "info":
			return b.plugins.Info(cmd.Args[1:]), true
		default:
			return fmt.Sprintf("Unknown subcommand: %s\n%s", cmd.Args[0], plugin.Usage()), true
		}
	})
}

func (b *Bot) pluginInstallCallbacks() plugin.InstallCallbacks {
	return plugin.InstallCallbacks{
		Confirm: func(source string, p *plugin.Plugin, isRemote bool) bool {
			return b.confirmInstall(source, p, isRemote)
		},
		Notify: b.notifyFn,
		SetPending: func(pending bool) {
			b.setPendingConfirmation(pending)
		},
		Unblock: b.unblockConfirm,
	}
}

func (b *Bot) refreshPluginSkills() {
	b.reloadSkills()
}

func (b *Bot) refreshSkillList() {
	if b.skills != nil {
		b.skills.RefreshList()
	}
}

func (b *Bot) unblockConfirm() {
	b.setPendingConfirmation(false)
	if b.confirmCh != nil {
		select {
		case b.confirmCh <- common.ConfirmRequest{Response: nil}:
		default:
		}
	}
}

func (b *Bot) confirmInstall(source string, p *plugin.Plugin, isRemote bool) bool {
	summary := plugin.ConfirmSummary(p, isRemote)
	if b.confirmFn == nil {
		b.unblockConfirm()
		return false
	}
	result := b.confirmFn(common.NewConfirmRequest("/plugin install", map[string]any{"source": source, "summary": summary}, common.LevelWrite))
	b.setPendingConfirmation(false)
	if !result && b.notifyFn != nil {
		b.notifyFn(fmt.Sprintf("Install cancelled: %s", source))
	}
	return result
}
