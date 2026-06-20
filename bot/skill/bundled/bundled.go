package bundled

import (
	ext "nekocode/bot/extension/skill/bundled"
	"nekocode/bot/skill"
)

func All() []*skill.Skill {
	return ext.All()
}
