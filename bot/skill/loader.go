package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultDirs returns the default skill directories (project + user).
func DefaultDirs() []string {
	var dirs []string
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, ".nekocode", "skills"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".nekocode", "skills"))
	}
	return dirs
}

// discoverSkills scans directories for skill.md / SKILL.md files.
func discoverSkills(dirs []string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.EqualFold(info.Name(), "skill.md") {
				abs, _ := filepath.Abs(path)
				if !seen[abs] {
					seen[abs] = true
					paths = append(paths, abs)
				}
				return filepath.SkipDir
			}
			return nil
		})
	}
	sort.Strings(paths)
	return paths
}

// LoadFromContent parses raw SKILL.md content into a Skill (for bundled skills).
func LoadFromContent(content string) (*Skill, error) {
	return parseSkillContent(content)
}

// loadSkill parses a SKILL.md file with its directory listing.
func loadSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	sk, err := parseSkillContent(string(data))
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	walkRoot := dir
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		walkRoot = filepath.Dir(realPath)
	}

	var files []string
	filepath.WalkDir(walkRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.EqualFold(name, "skill.md") || name == ".gitignore" || name == "README.md" || name == "LICENSE" {
			return nil
		}
		files = append(files, strings.Replace(p, walkRoot, dir, 1))
		return nil
	})

	sk.Dir = dir
	sk.Files = files
	return sk, nil
}

type frontmatter struct {
	Name                   string   `yaml:"name"`
	Description            string   `yaml:"description"`
	WhenToUse              string   `yaml:"when_to_use"`
	AllowedTools           []string `yaml:"allowed-tools"`
	Context                string   `yaml:"context"`
	Agent                  string   `yaml:"agent"`
	MaxSteps               int      `yaml:"max_steps"`
	TokenBudget            int      `yaml:"token_budget"`
	DisableModelInvocation bool     `yaml:"disable-model-invocation"`
}

func parseSkillContent(content string) (*Skill, error) {
	fm, body, err := parseFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if fm.Name == "" || fm.Description == "" {
		return nil, fmt.Errorf("missing required field: name or description")
	}
	return &Skill{
		Name:                   fm.Name,
		Description:            fm.Description,
		WhenToUse:              fm.WhenToUse,
		Content:                strings.TrimSpace(body),
		Context:                fm.Context,
		AgentType:              fm.Agent,
		AllowedTools:           fm.AllowedTools,
		MaxSteps:               fm.MaxSteps,
		TokenBudget:            fm.TokenBudget,
		DisableModelInvocation: fm.DisableModelInvocation,
	}, nil
}

func parseFrontmatter(content string) (*frontmatter, string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return nil, "", fmt.Errorf("frontmatter must start with ---")
	}
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return nil, "", fmt.Errorf("unclosed frontmatter (missing closing ---)")
	}
	yamlText := rest[:end]
	body := rest[end+4:]

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(yamlText), &fm); err != nil {
		return nil, "", fmt.Errorf("invalid YAML: %w", err)
	}
	return &fm, body, nil
}
