package builtin

import (
	"context"
	"fmt"
	"strings"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/tools"

	"nekocode/common"
)

// SubAgentFunc is the function signature for running a sub-agent.
type SubAgentFunc func(ctx context.Context, prompt, agentType, thoroughness string) (*subagent.Result, error)

type TaskTool struct {
	run SubAgentFunc
}

func NewTaskTool() *TaskTool { return &TaskTool{} }

func (t *TaskTool) Wire(run SubAgentFunc) {
	t.run = run
}

func (t *TaskTool) Name() string { return "task" }
func (t *TaskTool) ExecutionMode(map[string]any) tools.ExecutionMode { return tools.ModeParallel }
func (t *TaskTool) DangerLevel(map[string]any) common.DangerLevel     { return common.LevelSafe }
func (t *TaskTool) Description() string {
	return "Delegate multi-step work to an isolated sub-agent. Subagent cannot see your conversation — include full context in prompt. Types: researcher (search/analyze), executor (write/edit), verify (validate changes). For simple tasks (single file, one grep), use direct tools instead."
}

func (t *TaskTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "type", Type: "string", Required: true,
			Description: "researcher | executor | verify"},
		{Name: "prompt", Type: "string", Required: true,
			Description: "Self-contained task description with exact file paths and expected output."},
	}
}

func (t *TaskTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.run == nil {
		return "", fmt.Errorf("task tool: not wired")
	}

	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("missing prompt parameter")
	}

	typeName, ok := args["type"].(string)
	if !ok || typeName == "" {
		return "", fmt.Errorf("missing type parameter — must specify: explore, verify, executor, plan")
	}

	thoroughness := ""
	if len(prompt) < 300 {
		thoroughness = "quick"
	} else if len(prompt) > 1000 {
		thoroughness = "very thorough"
	}

	result, err := t.run(ctx, prompt, typeName, thoroughness)
	if err != nil && result == nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("task tool: subagent returned nil result")
	}

	out := subagent.FormatResult(result)
	if err != nil && result.Status == subagent.StatusPartial {
		out += fmt.Sprintf("\n\nNote: subagent was interrupted before completion: %v", err)
	}
	return out, nil
}
