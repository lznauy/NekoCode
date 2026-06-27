package app

import (
	"path/filepath"
	"sort"
	"strings"
)

type SkillManagementSnapshot struct {
	Skills  []SkillSnapshot  `json:"skills"`
	Plugins []PluginSnapshot `json:"plugins"`
}

type SkillSnapshot struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Files       []string `json:"files,omitempty"`
	Loaded      bool     `json:"loaded"`
	Source      string   `json:"source"`
	Plugin      string   `json:"plugin,omitempty"`
}

type PluginSnapshot struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Source      string   `json:"source,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Enabled     bool     `json:"enabled"`
	Skills      []string `json:"skills,omitempty"`
}

func (b *Bot) SkillManagementSnapshot() SkillManagementSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	plugins := b.pluginSnapshots()
	skills := b.skillSnapshots(plugins)
	return SkillManagementSnapshot{Skills: skills, Plugins: plugins}
}

func (b *Bot) SetPluginEnabled(name string, enabled bool) (SkillManagementSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	p, ok := b.pluginReg.Get(name)
	if !ok {
		return SkillManagementSnapshot{}, errPluginNotFound(name)
	}
	if enabled {
		if err := b.pluginReg.Enable(p.Name); err != nil {
			return SkillManagementSnapshot{}, err
		}
		if next, ok := b.pluginReg.Get(p.Name); ok {
			b.loadPluginExtensions(next)
		}
	} else {
		if err := b.pluginReg.Disable(p.Name); err != nil {
			return SkillManagementSnapshot{}, err
		}
		b.unloadPluginExtensions(p)
	}
	b.refreshPluginSkills()

	plugins := b.pluginSnapshots()
	skills := b.skillSnapshots(plugins)
	return SkillManagementSnapshot{Skills: skills, Plugins: plugins}, nil
}

func (b *Bot) RefreshSkillManagement() SkillManagementSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.pluginReg.LoadAll()
	b.refreshPluginSkills()
	plugins := b.pluginSnapshots()
	skills := b.skillSnapshots(plugins)
	return SkillManagementSnapshot{Skills: skills, Plugins: plugins}
}

func (b *Bot) pluginSnapshots() []PluginSnapshot {
	plugins := b.pluginReg.List()
	out := make([]PluginSnapshot, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, PluginSnapshot{
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Source:      p.Source,
			Dir:         p.Dir,
			Enabled:     p.Enabled,
			Skills:      p.SkillDirs(),
		})
	}
	return out
}

func (b *Bot) skillSnapshots(plugins []PluginSnapshot) []SkillSnapshot {
	skills := b.skillReg.List()
	out := make([]SkillSnapshot, 0, len(skills))
	for _, sk := range skills {
		source, pluginName := skillSource(sk.Dir, plugins)
		files := append([]string(nil), sk.Files...)
		sort.Strings(files)
		out = append(out, SkillSnapshot{
			Name:        sk.Name,
			Description: sk.Description,
			Dir:         sk.Dir,
			Files:       files,
			Loaded:      b.skillReg.IsLoaded(sk.Name),
			Source:      source,
			Plugin:      pluginName,
		})
	}
	return out
}

func skillSource(dir string, plugins []PluginSnapshot) (string, string) {
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

func errPluginNotFound(name string) error {
	return &pluginLookupError{name: name}
}

type pluginLookupError struct {
	name string
}

func (e *pluginLookupError) Error() string {
	return "plugin not found: " + e.name
}
