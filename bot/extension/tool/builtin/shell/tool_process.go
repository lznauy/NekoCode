package shell

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

const defaultProcessWaitTimeout = 5 * time.Minute

// ProcessTool operates on commands that shell has already returned as managed
// tasks. Waiting is event-driven inside the runtime; the model never polls.
type ProcessTool struct {
	shell       *ShellTool
	waitTimeout time.Duration
}

func NewProcessTool(shellTool *ShellTool) *ProcessTool {
	return &ProcessTool{shell: shellTool}
}

func (t *ProcessTool) Name() string { return "process" }

func (t *ProcessTool) Description() string {
	return "Manage a still-running task returned by shell. " +
		"Use wait when subsequent work needs the command to finish, watch when it needs the next output or exit event, list for recovery or inspection, and stop to terminate it. " +
		"wait and watch suspend this run until the event or a runtime safety boundary; wait_timeout leaves the process running. Do not poll."
}

func (t *ProcessTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "action", Type: "string", Required: true, Enum: []string{"list", "wait", "watch", "stop"}, Description: "Operation to perform."},
		{Name: "task", Type: "string", Required: false, Description: "Task name returned by shell. Required for wait, watch, and stop."},
		{Name: "event", Type: "string", Required: false, Enum: []string{"output", "exit"}, Description: "Event for watch; omit for other actions."},
	}
}

func (t *ProcessTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}

func (t *ProcessTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	if t.shell == nil {
		return core.ToolOutput{}, fmt.Errorf("process manager is unavailable")
	}
	action, err := toolutil.RequireStringArg(args, "action")
	if err != nil {
		return core.ToolOutput{}, err
	}
	action = strings.ToLower(strings.TrimSpace(action))
	registry := t.shell.registryOnce()
	owner := t.shell.CurrentSessionID()
	switch action {
	case "list":
		return core.ToolOutput{Text: formatProcessList(registry.list(owner))}, nil
	case "wait", "watch", "stop":
	default:
		return core.ToolOutput{}, fmt.Errorf("unknown action %q: must be list, wait, watch, or stop", action)
	}
	ref, err := toolutil.RequireStringArg(args, "task")
	if err != nil {
		return core.ToolOutput{}, err
	}
	switch action {
	case "wait":
		result, err := registry.wait(ctx, owner, ref, t.waitDuration())
		if err != nil {
			return core.ToolOutput{}, err
		}
		return core.ToolOutput{Text: formatProcessResult(result)}, nil
	case "watch":
		event, err := toolutil.RequireStringArg(args, "event")
		if err != nil {
			return core.ToolOutput{}, err
		}
		result, err := registry.watch(ctx, owner, ref, strings.ToLower(strings.TrimSpace(event)), t.waitDuration())
		if err != nil {
			return core.ToolOutput{}, err
		}
		return core.ToolOutput{Text: formatProcessResult(result)}, nil
	default: // stop; other values were rejected above.
		toolText, toolErr := registry.stop(owner, ref)
		return core.ToolOutput{Text: toolText}, toolErr
	}
}

func (t *ProcessTool) waitDuration() time.Duration {
	if t.waitTimeout > 0 {
		return t.waitTimeout
	}
	return defaultProcessWaitTimeout
}

func formatProcessResult(result processResult) string {
	var b strings.Builder
	if result.info.managed {
		fmt.Fprintf(&b, "task: %s\n", result.info.name)
	}
	fmt.Fprintf(&b, "status: %s\n", result.info.status)
	if result.info.status != "running" {
		fmt.Fprintf(&b, "exit_code: %d\n", result.info.exitCode)
	}
	if result.reason != "" {
		fmt.Fprintf(&b, "reason: %s\n", result.reason)
	}
	appendProcessOutput(&b, result.output)
	return b.String()
}

func formatProcessList(tasks []processInfo) string {
	if len(tasks) == 0 {
		return "(no managed processes)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-18s %-7s %-10s %s\n", "TASK", "STATUS", "ELAPSED", "COMMAND")
	for _, task := range tasks {
		fmt.Fprintf(&b, "%-18s %-7s %-10s %s\n",
			task.name, task.status, task.duration, compactProcessText(task.command, 60))
	}
	return b.String()
}
