package subagent

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentDef is parsed from an agents/*.md file (Claude Code format).
type AgentDef struct {
	Name         string   `yaml:"name"`
	Tools        []string `yaml:"tools"`
	SystemPrompt string   // markdown body (after frontmatter)
}

// ParseAgentMD parses a single agents/*.md file.
func ParseAgentMD(path string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent file: %w", err)
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.TrimSpace(content)

	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("missing frontmatter (---)")
	}

	rest := content[3:]
	yamlText, body, found := strings.Cut(rest, "\n---")
	if !found {
		return nil, fmt.Errorf("unclosed frontmatter")
	}
	body = strings.TrimSpace(body)

	var def AgentDef
	if err := yaml.Unmarshal([]byte(yamlText), &def); err != nil {
		return nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	if def.Name == "" {
		return nil, fmt.Errorf("missing required field: name")
	}
	def.SystemPrompt = body
	return &def, nil
}

// ToAgentType converts an AgentDef to AgentType for the subagent engine.
func (d *AgentDef) ToAgentType() AgentType {
	return AgentType{
		Name:         d.Name,
		SystemPrompt: d.SystemPrompt,
		Tools:        d.Tools,
	}
}

