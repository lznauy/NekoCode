package skill

import (
	"path/filepath"
	"sort"
	"strings"

	"nekocode/bot/plugin"
)

type ManagementSnapshot struct {
	Skills  []Snapshot        `json:"skills"`
	Plugins []plugin.Snapshot `json:"plugins"`
}

type Snapshot struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Files       []string `json:"files,omitempty"`
	Loaded      bool     `json:"loaded"`
	Source      string   `json:"source"`
	Plugin      string   `json:"plugin,omitempty"`
}

func BuildManagementSnapshot(reg *Registry, plugins []plugin.Snapshot) ManagementSnapshot {
	return ManagementSnapshot{
		Skills:  BuildSnapshots(reg, plugins),
		Plugins: plugins,
	}
}

func BuildSnapshots(reg *Registry, plugins []plugin.Snapshot) []Snapshot {
	if reg == nil {
		return nil
	}
	skills := reg.List()
	out := make([]Snapshot, 0, len(skills))
	for _, sk := range skills {
		source, pluginName := SourceForDir(sk.Dir, plugins)
		files := append([]string(nil), sk.Files...)
		sort.Strings(files)
		out = append(out, Snapshot{
			Name:        sk.Name,
			Description: sk.Description,
			Dir:         sk.Dir,
			Files:       files,
			Loaded:      reg.IsLoaded(sk.Name),
			Source:      source,
			Plugin:      pluginName,
		})
	}
	return out
}

func SourceForDir(dir string, plugins []plugin.Snapshot) (string, string) {
	if dir == "" {
		return "内置", ""
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
				return "插件", p.Name
			}
		}
	}
	return "本地", ""
}
