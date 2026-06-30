package app

import (
	"nekocode/bot/command"
	"nekocode/bot/extension/skill"
)

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
