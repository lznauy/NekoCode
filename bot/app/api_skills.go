package app

import botskill "nekocode/bot/skill"

func (b *Bot) SkillManagementSnapshot() botskill.ManagementSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.skillManagementSnapshot()
}

func (b *Bot) SetPluginEnabled(name string, enabled bool) (botskill.ManagementSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, err := b.plugins.SetEnabled(name, enabled); err != nil {
		return botskill.ManagementSnapshot{}, err
	}
	return b.skillManagementSnapshot(), nil
}

func (b *Bot) RefreshSkillManagement() botskill.ManagementSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.plugins.Reload()
	return b.skillManagementSnapshot()
}

func (b *Bot) skillManagementSnapshot() botskill.ManagementSnapshot {
	return b.skills.ManagementSnapshot(b.plugins.Snapshots(), b.plugins.MCPServers())
}
