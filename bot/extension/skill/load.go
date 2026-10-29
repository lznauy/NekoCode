package skill

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"nekocode/util/fs"
	"nekocode/util/yaml"

	goyaml "gopkg.in/yaml.v3"
)

// This file is the skill acquisition pipeline: discover SKILL.md files on
// disk (or embed the bundled one), read them, and parse them into Skill
// values.

// --- Discovery ---

// defaultDirs returns the default skill directories (project + user).
func defaultDirs() []string {
	return fs.NekocodeDirs("skills")
}

func discoverSkills(dirs []string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		_ = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
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
	// Walk is lexical within a directory; preserve directory precedence.
	return paths
}

// --- Loading ---

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

	sk.Dir = dir
	sk.Files = auxiliaryFiles(walkRoot, dir)
	return sk, nil
}

func auxiliaryFiles(walkRoot, dir string) []string {
	var files []string
	_ = filepath.WalkDir(walkRoot, func(p string, d os.DirEntry, err error) error {
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
	return files
}

// --- Parsing ---

type frontmatter struct {
	Name                   string   `yaml:"name"`
	Description            string   `yaml:"description"`
	AllowedTools           []string `yaml:"allowed-tools"`
	Context                string   `yaml:"context"`
	Agent                  string   `yaml:"agent"`
	MaxSteps               int      `yaml:"max_steps"`
	ContextWindow          int      `yaml:"context_window"`
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
	if strings.IndexFunc(fm.Name, unicode.IsSpace) >= 0 {
		return nil, fmt.Errorf("invalid skill name %q: must be usable as a $ command identifier and cannot contain whitespace", fm.Name)
	}
	return &Skill{
		Name:                   fm.Name,
		Description:            fm.Description,
		Content:                strings.TrimSpace(body),
		Context:                fm.Context,
		AgentProfile:           fm.Agent,
		AllowedTools:           fm.AllowedTools,
		MaxSteps:               fm.MaxSteps,
		ContextWindow:          fm.ContextWindow,
		DisableModelInvocation: fm.DisableModelInvocation,
	}, nil
}

func parseFrontmatter(content string) (*frontmatter, string, error) {
	yamlBytes, body, err := yaml.ParseYAMLFrontmatter(content)
	if err != nil {
		return nil, "", err
	}
	var fields map[string]goyaml.Node
	if err := goyaml.Unmarshal(yamlBytes, &fields); err != nil {
		return nil, "", fmt.Errorf("invalid YAML: %w", err)
	}
	for _, key := range []string{"allowed-tools", "agent", "max_steps", "context_window"} {
		if _, exists := fields[key]; exists {
			return nil, "", fmt.Errorf("unsupported execution field %q; configure execution limits in an Agent Profile", key)
		}
	}
	var fm frontmatter
	if err := goyaml.Unmarshal(yamlBytes, &fm); err != nil {
		return nil, "", fmt.Errorf("invalid YAML: %w", err)
	}
	if fm.Context != "" && fm.Context != "inline" {
		return nil, "", fmt.Errorf("unsupported context %q; only inline is supported", fm.Context)
	}
	return &fm, body, nil
}

// --- Bundled ---

//go:embed bundled/meta/SKILL.md
var bundledMetaContent string

//go:embed bundled/hunt/SKILL.md
var bundledHuntContent string

//go:embed bundled/think/SKILL.md
var bundledThinkContent string

//go:embed bundled/check/SKILL.md
var bundledCheckContent string

func bundledSkills() []*Skill {
	contents := []string{
		bundledMetaContent,
		bundledHuntContent,
		bundledThinkContent,
		bundledCheckContent,
	}
	sk := make([]*Skill, 0, len(contents))
	for _, content := range contents {
		parsed, err := parseSkillContent(content)
		if err == nil {
			sk = append(sk, parsed)
		}
	}
	return sk
}

// ParseContent validates skill text without registering it or reading auxiliary
// files. Diagnostic consumers share exactly the runtime frontmatter contract.
func ParseContent(content string) (*Skill, error) { return parseSkillContent(content) }
