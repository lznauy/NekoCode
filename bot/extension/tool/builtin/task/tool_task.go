package task

import (
	"context"
	"errors"
	"fmt"
	"nekocode/protocol"
	"strings"
	"unicode/utf8"

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
	return "Delegate independent, multi-step work to an isolated general-purpose sub-agent. Give the task a short title; the prompt is detailed instructions, not its display name. Choose a capability profile, then compose task-scoped skills: coder may modify the workspace; explore is strictly read-only. Custom plugin profiles are also accepted. Skills define workflow only and cannot expand the profile's tools. Include the exact goal, scope, relevant paths, constraints, and expected evidence. Use direct tools for simple or tightly coupled work, and verify returned claims before relying on them. Sub-agents cannot delegate further tasks: when parallel fan-out is needed, invoke multiple task tools directly from the parent agent instead of asking one sub-agent to dispatch child tasks."
}

func (t *TaskTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "isolation", Type: "string", Description: "Optional shared or worktree. worktree creates a private Git HEAD checkout (excludes uncommitted parent changes). Results stay there until the user applies them with /worktree. Omit on resume to reuse saved mode. Cannot weaken a worktree profile to shared."},
		{Name: "title", Type: "string", Description: "Short task display name, at most 80 characters. Do not copy the full prompt."},
		{Name: "resume_id", Type: "string", Description: "Continue a saved subagent in the current parent session using the full task_list items[].id or the returned [Subagent ID]. Never use latest_attempt.id: it identifies an execution attempt, not a subagent. Omit profile and skills to reuse the saved capability profile and skills. Never resume a task that is still running."},
		{Name: "profile", Type: "string",
			Description: "Required for new tasks; omit when resume_id is supplied to inherit the saved profile. Capability profile. Built-ins: coder (workspace write) and explore (strict read-only). Use agent_profiles to discover custom profiles; prefer their fully qualified plugin/name IDs."},
		{Name: "skills", Type: "array", Required: false,
			Description: "Task-scoped skill names. Skills cannot grant tools outside the selected profile.",
			Items:       &core.Schema{Type: "string"}},
		{Name: "prompt", Type: "string", Required: true,
			Description: "Self-contained task description with exact file paths and expected output."},
	}
}

func (t *TaskTool) Execute(ctx context.Context, args map[string]any) (output core.ToolOutput, runErr error) {
	var callback taskbridge.TaskCallbackFn
	switch cb := args["_sub_callback"].(type) {
	case taskbridge.TaskCallback:
		callback = cb.Callback
	case taskbridge.TaskCallbackFn:
		callback = cb
	}
	status := "failed"
	defer func() {
		if callback == nil {
			return
		}
		if runErr != nil {
			status = "failed"
		}
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || ctx.Err() != nil {
			status = "interrupted"
		}
		ev := protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentStatus: status}
		if runErr != nil {
			ev.IsError = true
		}
		callback(ev)
	}()
	if cb, ok := args["_sub_callback"].(taskbridge.TaskCallback); ok && cb.Begin != nil {
		if err := cb.Begin(ctx); err != nil {
			return core.ToolOutput{}, err
		}
	}
	if t.run == nil {
		return core.ToolOutput{}, fmt.Errorf("task tool: not wired")
	}

	prompt, err := toolutil.RequireStringArg(args, "prompt")
	if err != nil {
		return core.ToolOutput{}, err
	}
	title := strings.TrimSpace(toolutil.OptStringArg(args, "title", ""))
	if utf8.RuneCountInString(title) > 80 {
		return core.ToolOutput{}, fmt.Errorf("title must be at most 80 characters")
	}

	skills, err := stringSliceArg(args, "skills")
	if err != nil {
		return core.ToolOutput{}, err
	}
	resumeID := ""
	if raw, exists := args["resume_id"]; exists {
		var ok bool
		resumeID, ok = raw.(string)
		if !ok || strings.TrimSpace(resumeID) == "" {
			return core.ToolOutput{}, fmt.Errorf("resume_id must be a non-empty string")
		}
	}

	profile := ""
	if _, supplied := args["profile"]; supplied || resumeID == "" {
		profile, err = toolutil.RequireStringArg(args, "profile")
		if err != nil {
			return core.ToolOutput{}, fmt.Errorf("missing profile parameter — built-ins: coder, explore")
		}
	}

	isolation := toolutil.OptStringArg(args, "isolation", "")
	if raw, ok := args["isolation"]; ok {
		v, valid := raw.(string)
		if !valid || (v != "shared" && v != "worktree") {
			return core.ToolOutput{}, fmt.Errorf("isolation must be shared or worktree")
		}
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
		Isolation: isolation, Title: title, Prompt: prompt, Profile: profile, Skills: skills, ResumeID: resumeID,
	})
	if err != nil && result == nil {
		return core.ToolOutput{}, err
	}
	if result == nil {
		return core.ToolOutput{}, fmt.Errorf("task tool: subagent returned nil result")
	}

	output = core.ToolOutput{Text: result.Content}
	if result.ID != "" {
		output.Text = "[Subagent ID: " + result.ID + "]\n" + output.Text
	}
	switch result.Status {
	case taskbridge.TaskStatusCompleted:
		if err != nil {
			return output, fmt.Errorf("subagent reported completion with an error: %w", err)
		}
		status = "completed"
		return output, nil
	case taskbridge.TaskStatusPartial:
		status = "partial"
		output.Text = "[Subagent returned a partial result]\n" + output.Text
		if err != nil {
			return output, fmt.Errorf("subagent stopped before completion: %w", err)
		}
		return output, nil
	case taskbridge.TaskStatusFailed:
		if err == nil {
			err = fmt.Errorf("subagent failed")
		}
		return output, err
	default:
		return output, fmt.Errorf("subagent returned invalid status %d", result.Status)
	}
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
