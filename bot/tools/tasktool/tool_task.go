package tasktool

import (
	"context"
	"fmt"

	"nekocode/bot/tools"
	"nekocode/bot/tools/core"
	"nekocode/bot/tools/toolhelpers"
)

type TaskTool struct {
	toolhelpers.SafeReadOnlyTool
	run tools.TaskRunner
}

func NewTaskTool() *TaskTool { return &TaskTool{} }

func (t *TaskTool) Wire(run tools.TaskRunner) {
	t.run = run
}

func (t *TaskTool) Name() string { return "task" }
func (t *TaskTool) Description() string {
	return "Delegate multi-step work to an isolated sub-agent. Only the main agent can use this tool — sub-agents cannot spawn nested agents. Include full context in prompt since the subagent cannot see your conversation. Types: researcher (search/analyze), executor (write/edit), verify (validate changes). For simple tasks (single file, one grep), use direct tools instead."
}

func (t *TaskTool) Parameters() []core.Parameter {
	return []core.Parameter{
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

	prompt, err := toolhelpers.RequireStringArg(args, "prompt")
	if err != nil {
		return "", err
	}

	typeName, err := toolhelpers.RequireStringArg(args, "type")
	if err != nil {
		return "", fmt.Errorf("missing type parameter — must specify: researcher, executor, verify")
	}

	thoroughness := ""
	if len(prompt) < 300 {
		thoroughness = "quick"
	} else if len(prompt) > 1000 {
		thoroughness = "very thorough"
	}

	// Read sub-callback from args (injected by agent for TUI forwarding).
	subCtx := ctx
	if cb, ok := args["_sub_callback"].(tools.TaskCallbackFn); ok {
		subCtx = tools.WithTaskCallback(ctx, cb)
		delete(args, "_sub_callback") // clean up
	}
	result, err := t.run(subCtx, prompt, typeName, thoroughness)
	if err != nil && result == nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("task tool: subagent returned nil result")
	}

	out := result.Content
	if err != nil && result.Status == tools.TaskStatusPartial {
		out += fmt.Sprintf("\n\nNote: subagent was interrupted before completion: %v", err)
	}
	return out, nil
}
