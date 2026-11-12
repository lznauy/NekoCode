package shell

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

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
		"wait and watch suspend inference until the requested event, cancellation or an explicit host wait budget; there is no periodic model wake-up. wait_timeout, when a host budget is supplied, leaves the process running. Reuse its task ID, never restart shell just because waiting expired. new_output_bytes measures captured output since observation, not CPU/work progress. Repeated unchanged_waits calls for diagnosis or justified waiting, not blind polling or automatic killing."
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
		return processToolOutput(result), nil
	case "watch":
		event, err := toolutil.RequireStringArg(args, "event")
		if err != nil {
			return core.ToolOutput{}, err
		}
		result, err := registry.watch(ctx, owner, ref, strings.ToLower(strings.TrimSpace(event)), t.waitDuration())
		if err != nil {
			return core.ToolOutput{}, err
		}
		return processToolOutput(result), nil
	default: // stop; other values were rejected above.
		return registry.stop(owner, ref)
	}
}

func (t *ProcessTool) waitDuration() time.Duration {
	if t.waitTimeout > 0 {
		return t.waitTimeout
	}
	return 0 // No periodic timeout: the host waits without sampling the model.
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
	if result.info.managed && result.info.status == "running" {
		fmt.Fprintf(&b, "new_output_bytes: %d\nunchanged_waits: %d\n", result.observation.NewOutputBytes, result.observation.UnchangedWaits)
		if result.observation.UnchangedWaits < 2 {
			fmt.Fprintf(&b, "next_call: {\"action\":\"wait\",\"task\":%q}\n", result.info.name)
		}
		if result.observation.UnchangedWaits >= 2 {
			b.WriteString("diagnostic_hint: Repeated waits captured no new output. This does not prove the process is stuck. Inspect the retained output/state and choose justified waiting, an explicit stop, or report a blocker. Do not blindly repeat waits or relaunch the command.\n")
		} else if result.reason == "wait_timeout" {
			b.WriteString("wait_hint: Only this observation wait expired; the process is still running. Continue managing this task, not a new shell launch.\n")
		}
	}
	if result.reason == "reused_running" {
		b.WriteString("launch: reused existing process; no new command was started\n")
	}
	if result.info.status == "timeout" {
		b.WriteString("timeout_hint: The hard lifetime was reached and this process has exited. Waiting cannot resume it. Inspect the cause and partial side effects before deciding whether a changed retry is justified.\n")
	}
	appendProcessOutput(&b, result.output)
	if result.info.status == "failed" && possibleSandboxRestriction(result.output) {
		b.WriteString("recovery_hint: This failure may be caused by sandbox restrictions. Inspect the command and error, then retry shell with the minimal required outbound_domains, network, writable_roots, or sandbox_mode request to let the runtime obtain approval. Check partial side effects before retrying. Respect explicit denials and configured domain restrictions; do not assume host execution is required or stop at reporting the restriction.\n")
	}
	return b.String()
}

// These diagnostics are evidence for the agent to investigate, not authority
// to widen permissions or automatically replay a partially executed command.
func possibleSandboxRestriction(output string) bool {
	output = strings.ToLower(output)
	for _, diagnostic := range []string{
		"read-only file system", "readonly filesystem", "permission denied",
		"operation not permitted", "eacces", "eperm", "erofs",
		"network is unreachable", "network is down", "temporary failure in name resolution",
		"could not resolve host", "could not resolve proxy", "eai_again", "enotfound",
		"sandbox denied", "sandbox violation",
	} {
		if strings.Contains(output, diagnostic) {
			return true
		}
	}
	return false
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

func processToolOutput(result processResult) core.ToolOutput {
	return core.ToolOutput{Text: formatProcessResult(result), Process: result.observation}
}
