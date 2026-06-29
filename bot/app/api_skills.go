package app

import (
	"nekocode/bot/plugin"
	botskill "nekocode/bot/skill"
)

func (b *Bot) SkillManagementSnapshot() botskill.ManagementSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.SkillManagementSnapshot()
}

func (b *Bot) SetPluginEnabled(name string, enabled bool) (botskill.ManagementSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.SetPluginEnabled(name, enabled)
}

func (b *Bot) RefreshSkillManagement() botskill.ManagementSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.RefreshSkillManagement()
}

func skillPluginSnapshots(in []plugin.Snapshot) []botskill.PluginSnapshot {
	out := make([]botskill.PluginSnapshot, 0, len(in))
	for _, p := range in {
		out = append(out, botskill.PluginSnapshot{
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Source:      p.Source,
			Dir:         p.Dir,
			Enabled:     p.Enabled,
			Skills:      append([]string(nil), p.Skills...),
			SkillNames:  append([]string(nil), p.SkillNames...),
			Agents:      append([]string(nil), p.Agents...),
			Commands:    append([]string(nil), p.Commands...),
			MCPServers:  append([]string(nil), p.MCPServers...),
			HasHooks:    p.HasHooks,
		})
	}
	return out
}

func skillMCPSnapshots(in []plugin.MCPServerSnapshot) []botskill.MCPServerSnapshot {
	out := make([]botskill.MCPServerSnapshot, 0, len(in))
	for _, s := range in {
		out = append(out, botskill.MCPServerSnapshot{
			Name:          s.Name,
			Plugin:        s.Plugin,
			Command:       s.Command,
			Args:          append([]string(nil), s.Args...),
			DangerLevel:   s.DangerLevel,
			PluginEnabled: s.PluginEnabled,
		})
	}
	return out
}
