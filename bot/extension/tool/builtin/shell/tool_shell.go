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
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/sandbox"
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
	return "Execute a shell command in a permission-managed sandbox. " +
		"Commands that finish quickly return their output directly; commands still running after a short runtime-controlled observation return a task name and continue under process management. " +
		"Use the process tool only when you need to wait for, watch, list, or stop such a task. " +
		"A non-zero exit is reported as status: failed with exit_code; judge verification by that status, not by trailing success text. Pipelines use normal shell semantics, so preserve upstream failures with pipefail or separate commands when they matter. " +
		"Network, extra writable roots, and host execution require the corresponding minimal permission request. " +
		"By default commands run in an isolated workspace-write sandbox: the host filesystem is visible read-only (host binaries like npm globals and nix profiles are executable), but writes outside the workspace and authorized writable roots fail. For HTTP/HTTPS access to known domains, prefer outbound_domains with workspace-write or read-only mode. Use sandbox_mode=host only when unsandboxed host execution is needed; it cannot be combined with outbound_domains, including restrictions inherited from permissions.sandbox rules."
}

func (t *ShellTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "command", Type: "string", Required: true, Description: "Shell command to execute."},
		{Name: "name", Type: "string", Required: false, Description: "Optional stable name for a command that may keep running; otherwise the runtime assigns one."},
		{Name: "timeout_ms", Type: "number", Required: false, Description: "Optional hard lifetime. When reached, the runtime terminates the process group. Omit when the duration is unknown."},
		{Name: "sandbox_mode", Type: "string", Required: false, Enum: []string{"read-only", "workspace-write", "host"}, Description: "Defaults to workspace-write; read-only also prevents workspace writes. host requests unsandboxed execution and cannot be combined with outbound_domains, including an empty array or domains inherited from configuration."},
		{Name: "network", Type: "boolean", Required: false, Description: "Default false. Set true for broad outbound network access or host-network listening. For isolated HTTP/HTTPS to known domains, use outbound_domains instead; a domain list enables its allowed egress without network=true, and network=true cannot widen that list."},
		{Name: "outbound_domains", Type: "array", Items: &core.Schema{Type: "string"}, Description: "Optional exact DNS names, e.g. [\"example.com\"], for HTTP port 80 / HTTPS port 443 through the Linux native sandbox proxy. No URLs, IPs, ports or wildcards; list subdomains separately. Omit to use the network boolean; [] requires isolated networking with no egress. Use workspace-write or read-only, never host. Requested domains are intersected with matching permissions.sandbox rules; omitting this argument does not remove configured restrictions."},
		{Name: "writable_roots", Type: "array", Required: false, Items: &core.Schema{Type: "string"}, Description: "Existing directories outside the workspace. Verify each path exists and request the smallest directory needed; all descendants become writable. Omit workspace paths."},
	}
}

func (t *ShellTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	toolText, toolErr := t.run(ctx, args, core.PermissionRequest{})
	return core.ToolOutput{Text: toolText}, toolErr
}

func (t *ShellTool) ExecuteWithPermission(ctx context.Context, args map[string]any, grant core.PermissionRequest) (core.ToolOutput, error) {
	toolText, toolErr := t.run(ctx, args, grant)
	return core.ToolOutput{Text: toolText}, toolErr
}

func (t *ShellTool) run(ctx context.Context, args map[string]any, grant core.PermissionRequest) (string, error) {
	command, err := toolutil.RequireStringArg(args, "command")
	if err != nil {
		return "", err
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	workspace, err := workspacectx.WorkingDirectory(ctx)
	if err != nil {
		return "", err
	}
	reqProfile := t.effectiveSandboxRequest(args)
	if reqProfile.domainErr != nil {
		return "", reqProfile.domainErr
	}
	requestedCaps := reqProfile.permissionCapabilities()
	if hasCapability(requestedCaps, core.CapProcessHost) && !hasCapability(grant.Capabilities, core.CapProcessHost) {
		return "", permissionRequired(
			"shell command requests unsandboxed host execution",
			// "project" (not "once"): host grants are rememberable — jev-safe
			// can grant them automatically, so an explicit user "always allow"
			// must persist with the same durability.
			[]string{core.CapProcessHost}, "project", workspace, reqProfile.WritableRoots,
		)
	}
	if len(requestedCaps) > 0 && !containsAllCapabilities(grant.Capabilities, requestedCaps) {
		return "", core.RequiredPermissionError{Request: *t.PermissionPlan(args, workspace)}
	}

	host := hasCapability(grant.Capabilities, core.CapProcessHost)
	if reqProfile.OutboundDomains != nil && host {
		return "", fmt.Errorf(`outbound_domains cannot be used with host execution: host mode cannot enforce domain restrictions; use sandbox_mode="workspace-write" or "read-only". If unrestricted host execution is intended, remove outbound_domains from both the tool arguments and matching permissions.sandbox rules (an empty array still requires isolation)`)
	}
	if len(reqProfile.OutboundDomains) > 0 {
		have, domainErr := netpolicy.FromDetails(grant.Details)
		if domainErr != nil || !netpolicy.Covers(have, reqProfile.OutboundDomains) {
			return "", fmt.Errorf("network grant does not cover requested outbound domains")
		}
	}
	var profile sandbox.Profile
	if !host {
		profile, err = buildProfileFromRequest(workspace, reqProfile, grant.Capabilities)
		if err != nil {
			return "", err
		}
		if ruleProfile, ok := t.sandboxProfileFor(args); ok {
			if reqProfile.OutboundDomains == nil && ruleProfile.Network && !reqProfile.Network && hasCapability(grant.Capabilities, core.CapNetOutbound) {
				profile.Network = true
			}
		}
		if err := applyWorkspaceRoots(ctx, &profile, workspace); err != nil {
			return "", err
		}
	}
	timeout, err := optionalDurationArg(args, "timeout_ms")
	if err != nil {
		return "", err
	}

	task, initial, err := t.registryOnce().start(ctx, startRequest{
		name:       optStringArg(args, "name"),
		owner:      t.CurrentSessionID(),
		command:    command,
		directory:  workspace,
		profile:    profile,
		host:       host,
		timeout:    timeout,
		sampleWait: t.initialWaitDuration(),
	})
	if err != nil {
		var unavailable sandbox.UnavailableError
		if errors.As(err, &unavailable) {
			return "", hostPermission(err.Error(), workspace, nil)
		}
		return "", fmt.Errorf("failed to start shell command: %w", err)
	}
	info := task.summary()
	if info.managed {
		return formatManagedShellRun(info.name, initial), nil
	}
	if info.status == "done" {
		return initial, nil
	}
	return formatProcessResult(processResult{info: info, output: initial}), nil
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
