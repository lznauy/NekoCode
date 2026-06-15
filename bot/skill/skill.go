// Package skill provides a file-based skill system.
// Skills are directories with SKILL.md files with YAML frontmatter
// and Markdown body, discovered from project and user directories.
package skill

import (
	"fmt"
	"strings"
	"sync"

	"nekocode/common"
)

// Skill represents a loaded skill definition.
type Skill struct {
	Name        string
	Description string
	WhenToUse   string
	Content     string   // markdown body
	Dir         string   // absolute path to skill directory
	Files       []string // auxiliary files (excluding SKILL.md)

	Context                string   // "inline" or "fork"
	AgentType              string
	AllowedTools           []string
	MaxSteps               int
	ContextWindow            int
	DisableModelInvocation bool
}

// Registry manages loaded skills, thread-safe.
type Registry struct {
	*common.Registry[*Skill]
	loaded sync.Map // map[string]bool — tracks skills loaded in current session
}

func NewRegistry() *Registry {
	return &Registry{
		Registry: common.NewRegistry[*Skill](func(s *Skill) string { return s.Name }),
	}
}

func (r *Registry) RegisterBundled(skills []*Skill) {
	r.Registry.RegisterAll(skills)
}

func (r *Registry) Load(dirs []string) error {
	paths := discoverSkills(dirs)
	for _, p := range paths {
		sk, err := loadSkill(p)
		if err != nil {
			continue
		}
		if !r.Registry.Has(sk.Name) {
			r.Registry.Register(sk)
		}
	}
	return nil
}

func (r *Registry) MarkLoaded(name string) {
	r.loaded.Store(name, true)
}

func (r *Registry) ClearLoaded() {
	r.loaded.Clear()
}

func (r *Registry) IsLoaded(name string) bool {
	_, ok := r.loaded.Load(name)
	return ok
}

func (r *Registry) LoadedSet() map[string]bool {
	out := make(map[string]bool)
	r.loaded.Range(func(key, value any) bool {
		out[key.(string)] = true
		return true
	})
	return out
}

func (r *Registry) names() []string {
	return r.Registry.Names()
}

func (r *Registry) namesString() string {
	ns := r.names()
	if len(ns) == 0 {
		return "none"
	}
	return strings.Join(ns, ", ")
}

// BuildSkillListText generates the available-skills text injected into context.
func BuildSkillListText(skills []*Skill, loaded map[string]bool, contextWindow int) string {
	if len(skills) == 0 {
		return ""
	}
	maxChars := contextWindow / 100
	if maxChars < 500 {
		maxChars = 500
	}
	if maxChars > 3000 {
		maxChars = 3000
	}

	header := "## Available Skills (complete — no need to search for more)\n\n"
	header += "This is the authoritative list. Do NOT glob/grep/list to find skills — trust this list. Loaded skills are excluded:\n\n"

	var entries []string
	for _, sk := range skills {
		if loaded[sk.Name] {
			continue
		}
		entry := fmt.Sprintf("- **%s**: %s\n", sk.Name, sk.Description)
		if sk.WhenToUse != "" {
			entry += fmt.Sprintf("  When to use: %s\n", sk.WhenToUse)
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(header)
	remaining := maxChars - len([]rune(header))
	listed := 0
	for _, entry := range entries {
		n := len([]rune(entry))
		if remaining < n {
			if listed == 0 {
				sb.WriteString(entry)
				listed++
			}
			break
		}
		sb.WriteString(entry)
		remaining -= n
		listed++
	}
	if listed < len(entries) {
		fmt.Fprintf(&sb, "\n(%d more skills available but omitted due to token budget)\n", len(entries)-listed)
	}
	return sb.String()
}

// FormatForContext formats a skill's content for injection into conversation context.
func FormatForContext(sk *Skill) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<skill_content name=\"%s\">\n# Skill: %s\n\n", sk.Name, sk.Name)
	fmt.Fprintf(&sb, "**This skill is already loaded. Do NOT call the skill tool for %q.**\n\n", sk.Name)

	if sk.Dir != "" {
		fmt.Fprintf(&sb, "**Skill files: `%s`** — Read input files using absolute paths. Do NOT glob or search.\n", sk.Dir)
		sb.WriteString("**Output files go to the current working directory**, NOT the skill directory.\n\n")
	} else {
		sb.WriteString("(This is a built-in skill with no file-system directory.)\n\n")
	}
	sb.WriteString(sk.Content)

	if len(sk.Files) > 0 {
		sb.WriteString("\n\n## Skill files (absolute paths):\n")
		for _, f := range sk.Files {
			fmt.Fprintf(&sb, "- `%s`\n", f)
		}
	}
	sb.WriteString("</skill_content>")
	return sb.String()
}
