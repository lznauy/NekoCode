package skill

import ext "nekocode/bot/extension/skill"

type Skill = ext.Skill
type Registry = ext.Registry
type SkillTool = ext.SkillTool

func DefaultDirs() []string { return ext.DefaultDirs() }

func LoadFromContent(content string) (*Skill, error) { return ext.LoadFromContent(content) }

func NewRegistry() *Registry { return ext.NewRegistry() }

func BuildSkillListText(skills []*Skill, loaded map[string]bool, contextWindow int) string {
	return ext.BuildSkillListText(skills, loaded, contextWindow)
}

func FormatForContext(sk *Skill) string { return ext.FormatForContext(sk) }

func NewSkillTool(r *Registry) *SkillTool { return ext.NewSkillTool(r) }
