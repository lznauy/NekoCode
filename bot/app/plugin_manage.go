package app

import (
	"nekocode/bot/app/pluginops"
	"nekocode/bot/extension/skill"
	"nekocode/bot/plugincli"
)

func (b *Bot) pluginUninstall(args []string) (string, bool) {
	if len(args) == 0 {
		return "Usage: /plugin uninstall <name>", true
	}
	name := args[0]
	if p, ok := b.pluginReg.Get(name); ok {
		b.unloadPluginExtensions(p)
	}
	if err := b.pluginReg.Uninstall(name); err != nil {
		return pluginops.UninstallFailed(err), true
	}
	b.refreshPluginSkills()
	return pluginops.Uninstalled(name), true
}

func (b *Bot) pluginList(args []string) (string, bool) {
	return plugincli.FormatList(b.pluginReg.List()), true
}

func (b *Bot) pluginEnable(args []string) (string, bool) {
	lookup := pluginops.RequirePlugin(args, b.pluginReg.Get, "Usage: /plugin enable <name>")
	if !lookup.OK {
		return lookup.Message, true
	}
	p := lookup.Plugin
	if p.Enabled {
		return pluginops.AlreadyEnabled(p.Name), true
	}
	if err := b.pluginReg.Enable(p.Name); err != nil {
		return pluginops.EnableFailed(err), true
	}
	b.loadPluginExtensions(p)
	b.refreshPluginSkills()
	return pluginops.Enabled(p.Name), true
}

func (b *Bot) pluginDisable(args []string) (string, bool) {
	lookup := pluginops.RequirePlugin(args, b.pluginReg.Get, "Usage: /plugin disable <name>")
	if !lookup.OK {
		return lookup.Message, true
	}
	p := lookup.Plugin
	if !p.Enabled {
		return pluginops.AlreadyDisabled(p.Name), true
	}
	b.unloadPluginExtensions(p)
	if err := b.pluginReg.Disable(p.Name); err != nil {
		return pluginops.DisableFailed(err), true
	}
	b.refreshPluginSkills()
	return pluginops.Disabled(p.Name), true
}

func (b *Bot) pluginInfo(args []string) (string, bool) {
	lookup := pluginops.RequirePlugin(args, b.pluginReg.Get, "Usage: /plugin info <name>")
	if !lookup.OK {
		return lookup.Message, true
	}
	p := lookup.Plugin
	return plugincli.FormatInfo(p), true
}

func (b *Bot) refreshPluginSkills() {
	b.reloadSkills(b.skillReg.LoadedSet())
}

func (b *Bot) refreshSkillList() {
	b.ctxMgr.SetSkillList(skill.BuildSkillListText(b.skillReg.List(), b.skillReg.LoadedSet(), b.cfg.ContextWindow))
}
