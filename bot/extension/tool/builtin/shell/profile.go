package shell

import (
	"context"
	"fmt"
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/sandbox"
	"nekocode/bot/extension/tool/runtime/workspace"
)

type sandboxRequest struct {
	Mode            string
	OutboundDomains []string
	domainErr       error
	Network         bool
	WritableRoots   []string
}

// PermissionPlan describes the capability escalation implied by the shell's
// explicit sandbox arguments before execution. The executor uses this to fold
// command and capability approval into one atomic decision.
func (t *ShellTool) PermissionPlan(args map[string]any, workspace string) *core.PermissionRequest {
	request := t.effectiveSandboxRequest(args)
	if request.domainErr != nil {
		return nil
	}
	capabilities := request.permissionCapabilities()
	if len(capabilities) == 0 {
		return nil
	}
	scope := "project"
	// Note: no CapProcessHost special case — host grants are rememberable
	// like any other capability (see tool_shell.go).
	permission := &core.PermissionRequest{
		Reason:       fmt.Sprintf("shell command requests sandbox profile: %s", strings.Join(capabilities, ", ")),
		Capabilities: capabilities,
		Scope:        scope,
		Details: map[string]any{
			"workspace": workspace,
			"sandbox":   "native",
		},
	}
	if request.OutboundDomains != nil {
		permission.Details["outbound_domains"] = request.OutboundDomains
	}
	if len(request.WritableRoots) > 0 {
		permission.Details["writePaths"] = append([]string(nil), request.WritableRoots...)
	}
	return permission
}

func (r sandboxRequest) permissionCapabilities() []string {
	switch r.Mode {
	case "host":
		return []string{core.CapProcessHost}
	}
	var caps []string
	if r.Network {
		caps = append(caps, core.CapNetOutbound)
	}
	if len(r.WritableRoots) > 0 {
		caps = append(caps, core.CapFsWritePath)
	}
	return uniqueStrings(caps)
}

func buildProfileFromRequest(workspace string, req sandboxRequest, authorizedCaps []string) (sandbox.Profile, error) {
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "workspace-write"
	}
	profile := sandbox.Profile{Workspace: workspace, OutboundDomains: req.OutboundDomains}
	switch mode {
	case "workspace-write":
		// Default mode.
	case "read-only":
		profile.Mode = sandbox.ModeReadOnly
	case "host":
		return profile, fmt.Errorf("host execution must use RunHost")
	default:
		return profile, fmt.Errorf("unsupported sandbox_mode %q (want read-only, workspace-write, or host)", mode)
	}
	if req.Network && hasCapability(authorizedCaps, core.CapNetOutbound) {
		profile.Network = true
	}
	if len(req.WritableRoots) > 0 && hasCapability(authorizedCaps, core.CapFsWritePath) {
		profile.WritePaths = append(profile.WritePaths, req.WritableRoots...)
	}
	return profile, nil
}

// applyWorkspaceRoots mounts every authorized workspace root into the sandbox
// profile: read-write roots become WritePaths, read-only roots ReadPaths.
// Without this the sandbox only exposes the process cwd, and commands touching
// an added workspace fail with "cannot access". These roots are already
// user-authorized (same semantics as the file tools), so no capability check.
func applyWorkspaceRoots(ctx context.Context, profile *sandbox.Profile, cwd string) error {
	manager, ok := workspace.FromContext(ctx)
	if !ok {
		manager = workspace.New(cwd, nil)
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	for _, root := range manager.Snapshot() {
		if root.Path == cwd {
			continue // already covered by profile.Workspace
		}
		if info, err := os.Stat(root.Path); err != nil || !info.IsDir() {
			continue // stale root (deleted since authorization); mounting it would fail
		}
		if root.Access == workspace.AccessReadWrite {
			profile.WritePaths = append(profile.WritePaths, root.Path)
		} else {
			profile.ReadPaths = append(profile.ReadPaths, root.Path)
		}
	}
	return nil
}

func hasCapability(caps []string, target string) bool {
	return slices.Contains(caps, target)
}

func containsAllCapabilities(have, need []string) bool {
	for _, n := range need {
		if !hasCapability(have, n) {
			return false
		}
	}
	return true
}

func permissionRequired(reason string, caps []string, scope string, workspace string, writePaths []string) core.RequiredPermissionError {
	req := core.PermissionRequest{
		Reason:       reason,
		Capabilities: uniqueStrings(caps),
		Scope:        scope,
		Details: map[string]any{
			"workspace": workspace,
			"sandbox":   "native",
		},
	}
	if len(writePaths) > 0 {
		req.Details["writePaths"] = strings.Join(writePaths, ", ")
	}
	return core.RequiredPermissionError{Request: req}
}

func hostPermission(reason string, workspace string, writePaths []string) core.RequiredPermissionError {
	err := permissionRequired(
		fmt.Sprintf("%s; unsandboxed host execution is required", reason),
		[]string{core.CapProcessHost},
		"project",
		workspace,
		writePaths,
	)
	err.Request.Details["sandbox"] = "unavailable"
	return err
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (t *ShellTool) effectiveSandboxRequest(args map[string]any) sandboxRequest {
	r := sandboxRequestFromArgs(args)
	if profile, ok := t.sandboxProfileFor(args); ok && profile.OutboundDomains != nil {
		r.OutboundDomains = netpolicy.Intersect(r.OutboundDomains, profile.OutboundDomains)
	}
	if r.OutboundDomains != nil {
		if r.Mode == "host" {
			r.domainErr = fmt.Errorf(`outbound_domains cannot be used with host execution: host mode cannot enforce domain restrictions; use sandbox_mode="workspace-write" or "read-only". If unrestricted host execution is intended, remove outbound_domains from both the tool arguments and matching permissions.sandbox rules (an empty array still requires isolation)`)
		}
		r.Network = len(r.OutboundDomains) > 0
	}
	return r
}
