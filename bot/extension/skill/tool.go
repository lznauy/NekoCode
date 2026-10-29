package skill

import (
	"context"
	"fmt"
	"nekocode/bot/extension/tool/runtime/core"
)

// skillTool implements tools.Tool to let the model load skills by name.
type skillTool struct {
	registry *registry
}

// newSkillTool creates a skill tool bound to the given registry.
func newSkillTool(r *registry) *skillTool {
	return &skillTool{registry: r}
}

func (t *skillTool) Name() string { return "skill" }
func (t *skillTool) Description() string {
	return "Load a skill's instructions and workflows by name. Use when a task matches an available skill. If its <skill_content> is already present, follow it without reloading; a loaded skill may be reloaded only when compaction removed that content."
}

func (t *skillTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{
			Name:        "name",
			Type:        "string",
			Required:    true,
			Description: "The skill name to load, from the available skills list",
			Enum:        t.registry.ModelNames(),
		},
	}
}

func (t *skillTool) ExecutionMode(args map[string]any) core.ExecutionMode {
	return core.ModeSequential
}

func (t *skillTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return core.ToolOutput{}, fmt.Errorf("skill name is required")
	}

	sk, err := t.registry.ForModel(name)
	if err != nil {
		return core.ToolOutput{}, err
	}

	content := FormatForContext(sk)
	if len(content) > core.MaxOutputBytes {
		return core.ToolOutput{}, fmt.Errorf("skill %q was not loaded: complete instructions exceed the %d-byte tool output budget; shorten the skill before retrying", name, core.MaxOutputBytes)
	}
	// Context delivery records loaded state; Execute can still be deferred.
	return core.ToolOutput{Text: content}, nil
}
