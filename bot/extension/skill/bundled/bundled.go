// Package bundled provides built-in skills compiled into the NekoCode binary.
// These are always available regardless of file-system skill directories.
package bundled

import (
	_ "embed"

	"nekocode/bot/extension/skill"
)

//go:embed meta/SKILL.md
var metaContent string

// All returns all bundled skills, loaded from embedded SKILL.md files.
func All() []*skill.Skill {
	sk, err := skill.LoadFromContent(metaContent)
	if err != nil {
		return nil
	}
	return []*skill.Skill{sk}
}
