package evals

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type workflow struct{ Name, Content string }
type workflowSet struct{ skills map[string]*workflow }

func (s *workflowSet) Get(name string) (*workflow, bool) { w, ok := s.skills[name]; return w, ok }

// Read only the two named Markdown bodies as data. No skill discovery, YAML
// execution metadata, application registry, or project configuration is loaded.
func loadWorkflows(dir string) (*workflowSet, error) {
	set := &workflowSet{skills: map[string]*workflow{}}
	for _, name := range []string{"hunt", "check"} {
		data, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		if len(lines) < 3 || lines[0] != "---" {
			return nil, fmt.Errorf("%s: missing skill header", name)
		}
		end := 0
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				end = i
				break
			}
		}
		if end == 0 {
			return nil, fmt.Errorf("%s: unterminated skill header", name)
		}
		body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
		if body == "" {
			return nil, fmt.Errorf("%s: empty skill body", name)
		}
		set.skills[name] = &workflow{Name: name, Content: body}
	}
	return set, nil
}

func formatWorkflow(w *workflow) string {
	return fmt.Sprintf("<skill_content name=\"%s\">\n# Skill: %s\n\nThis is a runtime-selected workflow. Follow it for the current task, subject to system rules and the user's explicit request.\n\n**This skill is already loaded. Do NOT call the skill tool for %q.**\n\n(This is a built-in skill with no file-system directory.)\n\n%s</skill_content>", w.Name, w.Name, w.Name, w.Content)
}
