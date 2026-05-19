// Package skill provides a file-based skill system.
// Skills are directories with SKILL.md files with YAML frontmatter
// and Markdown body, discovered from project and user directories.
package skill

import (
	"fmt"
	"strings"
	"sync"
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
	TokenBudget            int
	DisableModelInvocation bool
}

// Registry manages loaded skills, thread-safe.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
	loaded map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill), loaded: make(map[string]bool)}
}

func (r *Registry) RegisterBundled(skills []*Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sk := range skills {
		if _, exists := r.skills[sk.Name]; !exists {
			r.skills[sk.Name] = sk
		}
	}
}

func (r *Registry) Load(dirs []string) error {
	paths := discoverSkills(dirs)
	for _, p := range paths {
		sk, err := loadSkill(p)
		if err != nil {
			continue
		}
		r.mu.Lock()
		if _, exists := r.skills[sk.Name]; !exists {
			r.skills[sk.Name] = sk
		}
		r.mu.Unlock()
	}
	return nil
}

func (r *Registry) Get(name string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sk, ok := r.skills[name]
	return sk, ok
}

func (r *Registry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Skill, 0, len(r.skills))
	for _, sk := range r.skills {
		out = append(out, sk)
	}
	return out
}

func (r *Registry) MarkLoaded(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded[name] = true
}

func (r *Registry) ClearLoaded() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded = make(map[string]bool)
}

func (r *Registry) IsLoaded(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loaded[name]
}

func (r *Registry) LoadedSet() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(r.loaded))
	for k, v := range r.loaded {
		out[k] = v
	}
	return out
}

func (r *Registry) names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for _, sk := range r.skills {
		names = append(names, sk.Name)
	}
	return names
}

func (r *Registry) namesString() string {
	ns := r.names()
	if len(ns) == 0 {
		return "none"
	}
	return strings.Join(ns, ", ")
}

// BuildSkillListText generates the available-skills text injected into context.
func BuildSkillListText(skills []*Skill, loaded map[string]bool, tokenBudget int) string {
	if len(skills) == 0 {
		return ""
	}
	maxChars := tokenBudget / 100
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
	sb.WriteString(fmt.Sprintf("<skill_content name=\"%s\">\n# Skill: %s\n\n", sk.Name, sk.Name))
	sb.WriteString(fmt.Sprintf("**This skill is already loaded. Do NOT call the skill tool for %q.**\n\n", sk.Name))

	if sk.Dir != "" {
		sb.WriteString(fmt.Sprintf("**Skill files: `%s`** — Read input files using absolute paths. Do NOT glob or search.\n", sk.Dir))
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
