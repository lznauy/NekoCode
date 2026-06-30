package app

import (
	"nekocode/common"
)

func (b *Bot) SkillManagementView() common.SkillManagementView {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.SkillManagementView()
}

func (b *Bot) SetPluginEnabled(name string, enabled bool) (common.SkillManagementView, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.SetPluginEnabled(name, enabled)
}

func (b *Bot) RefreshSkillManagement() common.SkillManagementView {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.RefreshSkillManagement()
}
