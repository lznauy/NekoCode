package shell

import (
	"context"
	"errors"
	"fmt"
	"math"
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"strings"
	"sync"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/sandbox"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/toolutil"
	workspacectx "nekocode/bot/extension/tool/runtime/workspace"
)

const (
	defaultShellInitialWait = 2 * time.Second
	maxDuration             = time.Duration(1<<63 - 1)
)

type ShellTool struct {
	registryInit sync.Once
	registry     *taskRegistry
	sessionMu    sync.RWMutex
	sessionID    string

	permEngine  *permission.Engine
	initialWait time.Duration
}

func (t *ShellTool) registryOnce() *taskRegistry {
	t.registryInit.Do(func() { t.registry = newTaskRegistry() })
	return t.registry
}

func (t *ShellTool) SetSandboxEngine(engine *permission.Engine) {
	t.permEngine = engine
}

func (t *ShellTool) Shutdown() error {
	return t.registryOnce().shutdown()
}

func (t *ShellTool) ProcessSummary() string {
	return t.registryOnce().summary(t.CurrentSessionID())
}

func (t *ShellTool) SetSessionID(id string) {
	t.sessionMu.Lock()
	t.sessionID = id
	t.sessionMu.Unlock()
}

func (t *ShellTool) CurrentSessionID() string {
	t.sessionMu.RLock()
	defer t.sessionMu.RUnlock()
	return t.sessionID
}

func (t *ShellTool) StopSession(id string) error {
	return t.registryOnce().stopOwner(id)
}

func (t *ShellTool) Name() string { return "shell" }

func (t *ShellTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}

func (t *ShellTool) Description() string {
	return "Run commands in a permission-managed sandbox (workspace-write, network off). For files you may edit, use read first; shell output gives no version receipt. " +
		"Long commands return a task for the process tool. Observation timeout leaves it running; timeout_ms kills the group. Identical anonymous live commands reuse a task. " +
		"Share files with other tools via workspace paths. In the Linux sandbox /tmp and isolated localhost are private to each command; $NEKOCODE_SCRATCH persists across calls in this actor/workspace with writable GOCACHE and XDG_CACHE_HOME, shell-only until toolbox close. " +
		"Static web app smoke test (Linux, Node + Chrome): node --input-type=module -e \"${NEKOCODE_BROWSER_CHECK:?Linux sandbox required}\" -- --static index.html 'readyPredicate' 'do:action()' 'observe:expression'. observe: prints the actual JSON value once; ordinary predicates assert true. This serves the entry directory, launches Chrome and cleans up in one call. Actions run once; assertions retry. For a custom backend run server and client in ONE command with cleanup and pass the loopback URL instead of --static. " +
		"Bash pipelines use pipefail: a failed check piped to tail still reports failure. For intentional early consumers like head, account for upstream SIGPIPE. Long output has a head/tail preview and readable transcript (8 MiB cap; incomplete captures marked). " +
		"Network, extra writable roots and host execution require approval. Check partial side effects before retrying with minimal permissions; respect denials and domain restrictions."
}

func (t *ShellTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "command", Type: "string", Required: true, Description: "Linux sandbox SVG/HTML screenshot (Node + Chrome): node --input-type=module -e \"${NEKOCODE_BROWSER_CHECK:?Linux sandbox required}\" -- --screenshot art.svg preview.png 900x640 (optional final argument X,Y,W,H crops the viewport; workspace PNG can be refreshed)."},
		{Name: "name", Type: "string", Required: false, Description: "Optional task name; use distinct names for intentional concurrent copies."},
		{Name: "timeout_ms", Type: "number", Required: false, Description: "Hard command lifetime in milliseconds. Omit if unknown."},
		{Name: "sandbox_mode", Type: "string", Required: false, Enum: []string{"read-only", "workspace-write", "host"}, Description: "Defaults to workspace-write; read-only also prevents workspace writes. host requests unsandboxed execution and cannot be combined with outbound_domains, including an empty array or domains inherited from configuration."},
		{Name: "network", Type: "boolean", Required: false, Description: "Default false. Set true for broad outbound network access or host-network listening. For isolated HTTP/HTTPS to known domains, use outbound_domains instead; a domain list enables its allowed egress without network=true, and network=true cannot widen that list."},
		{Name: "outbound_domains", Type: "array", Items: &core.Schema{Type: "string"}, Description: "Optional exact DNS names, e.g. [\"example.com\"], for HTTP port 80 / HTTPS port 443 through the Linux native sandbox proxy. No URLs, IPs, ports or wildcards; list subdomains separately. Omit to use the network boolean; [] requires isolated networking with no egress. Use workspace-write or read-only, never host. Requested domains are intersected with matching permissions.sandbox rules; omitting this argument does not remove configured restrictions."},
		{Name: "writable_roots", Type: "array", Required: false, Items: &core.Schema{Type: "string"}, Description: "Existing directories outside the workspace. Verify each path exists and request the smallest directory needed; all descendants become writable. Omit workspace paths."},
	}
}

func (t *ShellTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	return t.run(ctx, args, core.PermissionRequest{})
}

func (t *ShellTool) ExecuteWithPermission(ctx context.Context, args map[string]any, grant core.PermissionRequest) (core.ToolOutput, error) {
	return t.run(ctx, args, grant)
}

func (t *ShellTool) run(ctx context.Context, args map[string]any, grant core.PermissionRequest) (core.ToolOutput, error) {
	command, err := toolutil.RequireStringArg(args, "command")
	if err != nil {
		return core.ToolOutput{}, err
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return core.ToolOutput{}, fmt.Errorf("command is required")
	}

	workspace, err := workspacectx.WorkingDirectory(ctx)
	if err != nil {
		return core.ToolOutput{}, err
	}
	reqProfile := t.effectiveSandboxRequest(args)
	if reqProfile.validationErr != nil {
		return core.ToolOutput{}, reqProfile.validationErr
	}
	requestedCaps := reqProfile.permissionCapabilities()
	if hasCapability(requestedCaps, core.CapProcessHost) && !hasCapability(grant.Capabilities, core.CapProcessHost) {
		return core.ToolOutput{}, permissionRequired(
			"shell command requests unsandboxed host execution",
			// "project" (not "once"): host grants are rememberable — jev-safe
			// can grant them automatically, so an explicit user "always allow"
			// must persist with the same durability.
			[]string{core.CapProcessHost}, "project", workspace, reqProfile.WritableRoots,
		)
	}
	if len(requestedCaps) > 0 && !containsAllCapabilities(grant.Capabilities, requestedCaps) {
		return core.ToolOutput{}, core.RequiredPermissionError{Request: *t.PermissionPlan(args, workspace)}
	}

	host := hasCapability(grant.Capabilities, core.CapProcessHost)
	if reqProfile.OutboundDomains != nil && host {
		return core.ToolOutput{}, fmt.Errorf(`outbound_domains cannot be used with host execution: host mode cannot enforce domain restrictions; use sandbox_mode="workspace-write" or "read-only". If unrestricted host execution is intended, remove outbound_domains from both the tool arguments and matching permissions.sandbox rules (an empty array still requires isolation)`)
	}
	if len(reqProfile.OutboundDomains) > 0 {
		have, domainErr := netpolicy.FromDetails(grant.Details)
		if domainErr != nil || !netpolicy.Covers(have, reqProfile.OutboundDomains) {
			return core.ToolOutput{}, fmt.Errorf("network grant does not cover requested outbound domains")
		}
	}
	var profile sandbox.Profile
	if !host {
		profile, err = buildProfileFromRequest(workspace, reqProfile, grant.Capabilities)
		if err != nil {
			return core.ToolOutput{}, err
		}
		if ruleProfile, ok := t.sandboxProfileFor(args); ok {
			if reqProfile.OutboundDomains == nil && ruleProfile.Network && !reqProfile.Network && hasCapability(grant.Capabilities, core.CapNetOutbound) {
				profile.Network = true
			}
		}
		if err := applyWorkspaceRoots(ctx, &profile, workspace); err != nil {
			return core.ToolOutput{}, err
		}
	}
	timeout, err := optionalDurationArg(args, "timeout_ms")
	if err != nil {
		return core.ToolOutput{}, err
	}

	var outputStore *core.OutputStore
	if state := execution.ExecutionStateFromContext(ctx); state != nil {
		outputStore = state.OutputStore
	}
	initial, err := t.registryOnce().start(ctx, startRequest{
		outputStore: outputStore,
		name:        optStringArg(args, "name"),
		owner:       t.CurrentSessionID(),
		actor:       taskbridge.TaskIDFromCtx(ctx),
		command:     command,
		directory:   workspace,
		profile:     profile,
		host:        host,
		timeout:     timeout,
		sampleWait:  t.initialWaitDuration(),
	})
	if err != nil {
		var unavailable sandbox.UnavailableError
		if errors.As(err, &unavailable) {
			return core.ToolOutput{}, hostPermission(err.Error(), workspace, nil)
		}
		return core.ToolOutput{}, fmt.Errorf("failed to start shell command: %w", err)
	}
	if initial.reason == "reused_running" {
		return processToolOutput(initial), nil
	}
	info := initial.info
	if info.managed {
		if info.status != "running" {
			return processToolOutput(initial), nil
		}
		return core.ToolOutput{Text: formatManagedShellRun(info.name, initial.output), Process: initial.observation, Artifact: initial.captured.Artifact}, nil
	}
	if info.status == "done" {
		return core.ToolOutput{Text: initial.output + shellCompletionHints(initial), Process: initial.observation, Artifact: initial.captured.Artifact}, nil
	}
	return processToolOutput(initial), nil
}

func (t *ShellTool) initialWaitDuration() time.Duration {
	if t.initialWait > 0 {
		return t.initialWait
	}
	return defaultShellInitialWait
}

func (t *ShellTool) sandboxProfileFor(args map[string]any) (permission.SandboxProfile, bool) {
	if t.permEngine == nil {
		return permission.SandboxProfile{}, false
	}
	cmd, _ := args["command"].(string)
	callInfo := permission.BuildCallInfo("shell", map[string]any{"command": cmd}, "", "")
	return t.permEngine.SandboxFor("shell", callInfo)
}

func optionalDurationArg(args map[string]any, key string) (time.Duration, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0, nil
	}
	var ms float64
	switch value := raw.(type) {
	case float64:
		ms = value
	case int:
		ms = float64(value)
	default:
		return 0, fmt.Errorf("%s must be a positive number", key)
	}
	if ms <= 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
		return 0, fmt.Errorf("%s must be a positive number", key)
	}
	if ms > float64(maxDuration/time.Millisecond) {
		return 0, fmt.Errorf("%s is too large", key)
	}
	duration := time.Duration(ms * float64(time.Millisecond))
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be at least 0.000001ms", key)
	}
	return duration, nil
}

func formatManagedShellRun(task, output string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "task: %s\n", task)
	b.WriteString("status: running\nlaunch_hint: Use process to wait for completion. For persistent services, probe readiness and run clients in the same shell command (isolated localhost).\n")
	appendProcessOutput(&b, output)
	return b.String()
}

func appendProcessOutput(b *strings.Builder, output string) {
	if strings.TrimSpace(output) == "" {
		return
	}
	b.WriteString("output:\n")
	b.WriteString(output)
	if !strings.HasSuffix(output, "\n") {
		b.WriteByte('\n')
	}
}
