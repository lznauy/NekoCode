package app

import (
	"fmt"
	"os"

	"nekocode/bot/app/pluginruntime"
	"nekocode/bot/debug"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/skill"
	"nekocode/bot/extension/skill/bundled"
)

func (b *Bot) initSkills() {
	b.reloadSkills(nil)
}

func (b *Bot) reloadSkills(loaded map[string]bool) {
	b.skillReg = skill.NewRegistry()
	b.skillReg.RegisterBundled(bundled.All())
	dirs := skill.DefaultDirs()
	for _, d := range b.pluginReg.SkillDirs() {
		dirs = append(dirs, d)
	}
	if err := b.skillReg.Load(dirs); err != nil {
		fmt.Fprintf(os.Stderr, "skill: load error: %v\n", err)
	}
	b.ctxMgr.SetSkillList(skill.BuildSkillListText(b.skillReg.List(), loaded, b.cfg.ContextWindow))
	b.toolRegistry.Unregister("skill")
	b.toolRegistry.Register(skill.NewSkillTool(b.skillReg))
}

func (b *Bot) initPlugins() {
	b.pluginReg = plugin.NewRegistry(plugin.DefaultDirs())
	b.pluginReg.Logf = debug.Log
	b.mcpClients = make(map[string]*mcp.Client)

	b.pluginReg.LoadAll()
	for _, p := range b.pluginReg.List() {
		if p.Enabled {
			b.loadPluginExtensions(p)
		}
	}
}

func (b *Bot) loadPluginExtensions(p *plugin.Plugin) {
	b.pluginRuntime().Load(p)
}

func (b *Bot) unloadPluginExtensions(p *plugin.Plugin) {
	b.pluginRuntime().Unload(p)
}

func (b *Bot) pluginRuntime() pluginruntime.Runtime {
	return pluginruntime.Runtime{
		Hooks:      b.hookReg,
		Tools:      b.toolRegistry,
		MCPClients: b.mcpClients,
		Logf:       debug.Log,
	}
}
