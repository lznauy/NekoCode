package skill

import (
	"path/filepath"
	"sort"
	"strings"

	"nekocode/common"
)

func BuildManagementView(reg *Registry, plugins []common.PluginView, mcp []common.MCPServerView) common.SkillManagementView {
	return common.SkillManagementView{
		Skills:  BuildViews(reg, plugins),
		Plugins: plugins,
		MCP:     mcp,
	}
}

func BuildViews(reg *Registry, plugins []common.PluginView) []common.SkillView {
	if reg == nil {
		return nil
	}
	skills := reg.List()
	out := make([]common.SkillView, 0, len(skills))
	for _, sk := range skills {
		kind, source, pluginName := SourceForDir(sk.Dir, plugins)
		files := append([]string(nil), sk.Files...)
		sort.Strings(files)
		out = append(out, common.SkillView{
			Name:        sk.Name,
			Description: sk.Description,
			Dir:         sk.Dir,
			Files:       files,
			Loaded:      reg.IsLoaded(sk.Name),
			Source:      source,
			SourceKind:  kind,
			Plugin:      pluginName,
		})
	}
	return out
}

// SourceForDir classifies a skill directory into one of three kinds:
//   - "builtin": embedded/bundled skill (empty dir)
//   - "plugin":  lives under a plugin's skill directory
//   - "local":   a standalone file-system skill (e.g. ~/.nekocode/skills/...)
//
// It returns (kind, label, pluginName). label is a Chinese display string
// ("内置" / "插件" / "本地"); kind is the stable machine-readable value.
func SourceForDir(dir string, plugins []common.PluginView) (string, string, string) {
	if dir == "" {
		return "builtin", "内置", ""
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	for _, p := range plugins {
		for _, skillDir := range p.Skills {
			absSkillDir, err := filepath.Abs(skillDir)
			if err != nil {
				absSkillDir = skillDir
			}
			if absDir == absSkillDir || strings.HasPrefix(absDir, absSkillDir+string(filepath.Separator)) {
				return "plugin", "插件", p.Name
			}
		}
	}
	return "local", "本地", ""
}
