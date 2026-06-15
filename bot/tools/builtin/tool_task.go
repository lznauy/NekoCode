package builtin

import (
	"context"
	"fmt"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/tools"
)

// SubAgentFunc is the function signature for running a sub-agent.
type SubAgentFunc func(ctx context.Context, prompt, agentType, thoroughness string) (*subagent.Result, error)

type TaskTool struct {
	SafeReadOnlyTool
	run SubAgentFunc
}

func NewTaskTool() *TaskTool { return &TaskTool{} }

func (t *TaskTool) Wire(run SubAgentFunc) {
	t.run = run
}

func (t *TaskTool) Name() string { return "task" }
func (t *TaskTool) Description() string {
	return "Delegate multi-step work to an isolated sub-agent. Only the main agent can use this tool — sub-agents cannot spawn nested agents. Include full context in prompt since the subagent cannot see your conversation. Types: researcher (search/analyze), executor (write/edit), verify (validate changes). For simple tasks (single file, one grep), use direct tools instead."
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

	prompt, err := requireStringArg(args, "prompt")
	if err != nil {
		return "", err
	}

	typeName, err := requireStringArg(args, "type")
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
	if cb, ok := args["_sub_callback"].(subagent.SubCallbackFn); ok {
		subCtx = subagent.WithSubCallback(ctx, cb)
		delete(args, "_sub_callback") // clean up
	}
	result, err := t.run(subCtx, prompt, typeName, thoroughness)
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
