package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadFromContent parses raw SKILL.md content into a Skill.
func LoadFromContent(content string) (*Skill, error) {
	return parseSkillContent(content)
}

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
	return files
}
