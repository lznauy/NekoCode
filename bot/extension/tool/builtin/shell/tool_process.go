package shell

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

const defaultProcessObservationWait = 30 * time.Second
const maxProcessObservationWait = 2 * time.Minute

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
		"wait and watch return on the requested event, cancellation or one bounded observation deadline (default 30 seconds, optional wait_timeout_ms capped at 120000). A larger value is capped, not rejected. wait_timeout leaves the process running; it does not mean the command failed. The host does not automatically poll or repeat the call. For a persistent server, check readiness rather than waiting for exit or assuming a new log line will arrive. Reuse its task ID, never restart shell just because waiting expired. new_output_bytes measures captured output since observation, not CPU/work progress. Repeated unchanged_waits calls for diagnosis or justified waiting, not blind polling or automatic killing."
}

func (t *ProcessTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "action", Type: "string", Required: true, Enum: []string{"list", "wait", "watch", "stop"}, Description: "Operation to perform."},
		{Name: "task", Type: "string", Required: false, Description: "Managed process name from shell or process(action=list). Required for wait, watch, and stop; omit for list. Delegated task IDs belong to task_list/task_result."},
		{Name: "wait_timeout_ms", Type: "number", Required: false, Description: "Optional positive observation budget in milliseconds; values over 120000 are capped. Default 30000. Only wait/watch. Expiry returns running state without terminating the process. This is separate from shell.timeout_ms, which is a hard lifetime."},
		{Name: "event", Type: "string", Required: false, Enum: []string{"output", "exit"}, Description: "Required for watch: output waits for new output or termination; exit waits for termination. Omit for other actions."},
	}
}

func (t *ProcessTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}

// WaitReady checks whether wait/watch can return without waiting for a future
// process event. It uses the caller's registry and leaves output cursors intact.
func (t *ProcessTool) WaitReady(args map[string]any) bool {
	if t.shell == nil {
		return true // Execute reports the unavailable manager.
	}
	ref, _ := args["task"].(string)
	task, err := t.shell.registryOnce().task(t.shell.CurrentSessionID(), ref)
	if err != nil {
		return true // Preserve the tool's normal missing-handle error.
	}
	action, _ := args["action"].(string)
	event, _ := args["event"].(string)
	watchOutput := strings.EqualFold(strings.TrimSpace(action), "watch") && strings.EqualFold(strings.TrimSpace(event), "output")
	task.mu.Lock()
	defer task.mu.Unlock()
	return task.status != taskRunning || task.monitoring || (watchOutput && task.watchOffset < task.logEnd)
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
	if _, supplied := args["event"]; supplied && action != "watch" {
		return core.ToolOutput{}, fmt.Errorf("event is only valid for watch; omit it for %s", action)
	}
	if _, supplied := args["task"]; supplied && action == "list" {
		return core.ToolOutput{}, fmt.Errorf("list shows all managed processes; omit task, or use wait/watch with task to observe one process")
	}
	observationWait := t.waitDuration()
	waitCapped := false
	if raw, supplied := args["wait_timeout_ms"]; supplied {
		if action != "wait" && action != "watch" {
			return core.ToolOutput{}, fmt.Errorf("wait_timeout_ms is only valid for wait/watch")
		}
		if raw == nil {
			return core.ToolOutput{}, fmt.Errorf("wait_timeout_ms must be a positive number")
		}
		duration, err := optionalDurationArg(args, "wait_timeout_ms")
		if err != nil {
			return core.ToolOutput{}, err
		}
		if duration > maxProcessObservationWait {
			duration = maxProcessObservationWait
			waitCapped = true
		}
		observationWait = duration
	}
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
		result, err := registry.wait(ctx, owner, ref, observationWait)
		if err != nil {
			return core.ToolOutput{}, err
		}
		return withWaitCapNotice(processToolOutput(result), waitCapped), nil
	case "watch":
		event, err := toolutil.RequireStringArg(args, "event")
		if err != nil {
			return core.ToolOutput{}, err
		}
		result, err := registry.watch(ctx, owner, ref, strings.ToLower(strings.TrimSpace(event)), observationWait)
		if err != nil {
			return core.ToolOutput{}, err
		}
		return withWaitCapNotice(processToolOutput(result), waitCapped), nil
	default: // stop; other values were rejected above.
		return registry.stop(owner, ref)
	}
}

func withWaitCapNotice(output core.ToolOutput, capped bool) core.ToolOutput {
	if capped {
		output.Text = "wait_timeout_ms exceeded the observation limit; this call waited at most 120000 ms. The process remains managed normally.\n" + output.Text
	}
	return output
}

func (t *ProcessTool) waitDuration() time.Duration {
	if t.waitTimeout > 0 {
		return t.waitTimeout
	}
	return defaultProcessObservationWait
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
		if result.observation.UnchangedWaits >= 2 {
			b.WriteString("diagnostic_hint: Repeated waits captured no new output. This does not prove the process is stuck. Inspect the retained output/state and choose justified waiting, an explicit stop, or report a blocker. Do not blindly repeat waits or relaunch the command.\n")
		} else if result.reason == "wait_timeout" {
			b.WriteString("wait_hint: Only this observation wait expired; the process is still running. For a persistent server, test service readiness instead of waiting for exit or another log line. Default isolated shell calls cannot reach another call's localhost: run server and readiness/client checks in the same command, or request network=true when host-network access is needed. Do not blindly repeat waits or relaunch an existing process.\n")
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
	b.WriteString(shellCompletionHints(result))
	return b.String()
}

func shellCompletionHints(result processResult) string {
	if result.info.status != "done" && result.info.status != "failed" {
		return ""
	}
	switch result.observation.TestReportState {
	case "pending":
		return "\nverification_hint: Go test output is still draining. Use process wait on the returned task before accepting the check.\n"
	case "unreadable":
		return "\nverification_hint: Go test output could not be read completely. Review the check before acceptance.\n"
	}
	if result.observation.TestName != "" {
		return fmt.Sprintf("\nverification: Go test %s: %s\n", result.observation.TestName, result.observation.TestOutcome)
	}
	if result.observation.NoTestsRun {
		return "\nverification_hint: Go reported zero tests. Check the package and test selector before claiming test coverage.\n"
	}
	if result.info.hasScratch && missingTempInput(result.output, result.info.directory) {
		return "\ntemp_hint: A /tmp input was reported missing. If created in an earlier isolated command, recreate it under $NEKOCODE_SCRATCH and use that path in later calls.\n"
	}
	return ""
}

// Match a reported missing /tmp operand, not paths from unrelated command output.
var missingTmpPath = regexp.MustCompile(`(?:^|[[:space:]'"])(/tmp/[^[:space:]:'"]+)`)

func missingTempInput(output, workspace string) bool {
	if !strings.Contains(output, "No such file or directory") {
		return false
	}
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.Contains(line, "No such file or directory") {
			continue
		}
		for _, match := range missingTmpPath.FindAllStringSubmatch(line, -1) {
			path := filepath.Clean(match[1])
			if !strings.HasPrefix(path, "/tmp/") {
				continue
			}
			relative, err := filepath.Rel(workspace, path)
			if err == nil && !filepath.IsLocal(relative) {
				return true
			}
		}
	}
	return false
}

var sandboxErrno = regexp.MustCompile(`\b(?:eacces|eperm|erofs|eai_again|enotfound)\b`)

// These diagnostics are evidence for the agent to investigate, not authority
// to widen permissions or automatically replay a partially executed command.
func possibleSandboxRestriction(output string) bool {
	output = strings.ToLower(output)
	for _, diagnostic := range []string{
		"read-only file system", "readonly filesystem", "permission denied",
		"operation not permitted",
		"network is unreachable", "network is down", "temporary failure in name resolution",
		"could not resolve host", "could not resolve proxy",
		"sandbox denied", "sandbox violation",
	} {
		if strings.Contains(output, diagnostic) {
			return true
		}
	}
	return sandboxErrno.MatchString(output)
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
	return core.ToolOutput{Text: formatProcessResult(result), Process: result.observation, Artifact: result.captured.Artifact}
}
