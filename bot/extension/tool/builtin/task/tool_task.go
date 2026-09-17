package task

import (
	"context"
	"fmt"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

type TaskTool struct {
	toolutil.SafeReadOnlyTool
	run taskbridge.TaskRunner
}

func NewTaskTool() *TaskTool { return &TaskTool{} }

func (t *TaskTool) Wire(run taskbridge.TaskRunner) {
	t.run = run
}

func (t *TaskTool) Name() string { return "task" }
func (t *TaskTool) Description() string {
	return "Delegate independent, multi-step work to an isolated general-purpose sub-agent. Choose a capability profile, then compose task-scoped skills: coder may modify the workspace; explore is strictly read-only. Custom plugin profiles are also accepted. Skills define workflow only and cannot expand the profile's tools. Include the exact goal, scope, relevant paths, constraints, and expected evidence. Use direct tools for simple or tightly coupled work, and verify returned claims before relying on them."
}

func (t *TaskTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "profile", Type: "string", Required: true,
			Description: "Capability profile. Built-ins: coder (workspace write) and explore (strict read-only). Use agent_profiles to discover custom profiles; prefer their fully qualified plugin/name IDs."},
		{Name: "skills", Type: "array", Required: false,
			Description: "Task-scoped skill names. Skills cannot grant tools outside the selected profile.",
			Items:       &core.Schema{Type: "string"}},
		{Name: "prompt", Type: "string", Required: true,
			Description: "Self-contained task description with exact file paths and expected output."},
	}
}

func (t *TaskTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.run == nil {
		return "", fmt.Errorf("task tool: not wired")
	}

	prompt, err := toolutil.RequireStringArg(args, "prompt")
	if err != nil {
		return "", err
	}

	profile, err := toolutil.RequireStringArg(args, "profile")
	if err != nil {
		return "", fmt.Errorf("missing profile parameter — built-ins: coder, explore")
	}
	skills, err := stringSliceArg(args, "skills")
	if err != nil {
		return "", err
	}

	// Read sub-callback from args (injected by agent for TUI forwarding).
	subCtx := ctx
	switch cb := args["_sub_callback"].(type) {
	case taskbridge.TaskCallback:
		subCtx = taskbridge.WithTaskID(taskbridge.WithTaskCallback(ctx, cb.Callback), cb.ID)
		delete(args, "_sub_callback")
	case taskbridge.TaskCallbackFn:
		subCtx = taskbridge.WithTaskCallback(ctx, cb)
		delete(args, "_sub_callback")
	}

	result, err := t.run(subCtx, taskbridge.TaskSpec{
		Prompt: prompt, Profile: profile, Skills: skills,
	})
	if err != nil && result == nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("task tool: subagent returned nil result")
	}

	out := result.Content
	if err != nil && result.Status == taskbridge.TaskStatusPartial {
		out += fmt.Sprintf("\n\nNote: subagent was interrupted before completion: %v", err)
	}
	return out, nil
}

func stringSliceArg(args map[string]any, name string) ([]string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]string); ok {
			return append([]string(nil), typed...), nil
		}
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok || value == "" {
			return nil, fmt.Errorf("%s must contain non-empty strings", name)
		}
		out = append(out, value)
	}
	return out, nil
}
