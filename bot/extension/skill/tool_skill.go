package skill

import (
	"context"
	"fmt"
	"nekocode/common"

	"nekocode/bot/tools"
)

// SkillTool implements tools.Tool to let the model load skills by name.
type SkillTool struct {
	registry *Registry
	onLoad   func(name string) // called after a skill is loaded via tool
}

// NewSkillTool creates a skill tool bound to the given registry.
func NewSkillTool(r *Registry) *SkillTool {
	return &SkillTool{registry: r}
}

// SetOnLoad sets a callback invoked after a skill is successfully loaded via this tool.
func (t *SkillTool) SetOnLoad(fn func(name string)) { t.onLoad = fn }

func (t *SkillTool) Name() string { return "skill" }
func (t *SkillTool) Description() string {
	return "Load a skill's instructions and workflows by name. Use when a task matches an available skill. Do NOT reload a skill already loaded via slash command — if the skill content is already in context, use it directly."
}

func (t *SkillTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{
			Name:        "name",
			Type:        "string",
			Required:    true,
			Description: "The skill name to load, from the available skills list",
		},
	}
}

func (t *SkillTool) ExecutionMode(args map[string]any) tools.ExecutionMode {
	return tools.ModeSequential
}

func (t *SkillTool) DangerLevel(args map[string]any) common.DangerLevel {
	return common.LevelSafe
}

func (t *SkillTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return "", fmt.Errorf("skill name is required")
	}

	sk, ok := t.registry.Get(name)
	if !ok {
		return "", fmt.Errorf("skill not found: %s (available: %s)", name, t.registry.namesString())
	}

	// If already loaded, tell the model to use the existing content.
	if t.registry.IsLoaded(name) {
		return fmt.Sprintf("Skill %q is already loaded in this conversation. Use its instructions directly — do NOT call the skill tool again.", name), nil
	}

	// Notify that this skill is now loaded.
	if t.onLoad != nil {
		t.onLoad(name)
	}

	return FormatForContext(sk), nil
}
