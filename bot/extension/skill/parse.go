package skill

import (
	"fmt"
	"strings"

	"nekocode/common"

	"gopkg.in/yaml.v3"
)

type frontmatter struct {
	Name                   string   `yaml:"name"`
	Description            string   `yaml:"description"`
	WhenToUse              string   `yaml:"when_to_use"`
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
	return &Skill{
		Name:                   fm.Name,
		Description:            fm.Description,
		WhenToUse:              fm.WhenToUse,
		Content:                strings.TrimSpace(body),
		Context:                fm.Context,
		AgentType:              fm.Agent,
		AllowedTools:           fm.AllowedTools,
		MaxSteps:               fm.MaxSteps,
		ContextWindow:          fm.ContextWindow,
		DisableModelInvocation: fm.DisableModelInvocation,
	}, nil
}

func parseFrontmatter(content string) (*frontmatter, string, error) {
	yamlBytes, body, err := common.ParseYAMLFrontmatter(content)
	if err != nil {
		return nil, "", err
	}
	var fm frontmatter
	if err := yaml.Unmarshal(yamlBytes, &fm); err != nil {
		return nil, "", fmt.Errorf("invalid YAML: %w", err)
	}
	return &fm, body, nil
}
