package app

import (
	"nekocode/bot/debug"
	"nekocode/bot/extension/skill"
)

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
