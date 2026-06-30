package skill

import _ "embed"

//go:embed bundled/meta/SKILL.md
var bundledMetaContent string

func BundledSkills() []*Skill {
	sk, err := LoadFromContent(bundledMetaContent)
	if err != nil {
		return nil
	}
	return []*Skill{sk}
}
