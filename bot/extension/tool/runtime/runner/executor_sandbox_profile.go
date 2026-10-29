package runner

import (
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"strings"

	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
)

func (e *Executor) permissionRequestFromEntry(entry tools.Entry, args map[string]any) *core.PermissionRequest {
	if entry.PermissionPlan == nil {
		return nil
	}
	_, workspace, _ := e.permissionEngine()
	return entry.PermissionPlan(args, workspace)
}

func (e *Executor) applySandboxProfile(tc core.ToolCallItem) core.ToolCallItem {
	engine, ws, home := e.permissionEngine()
	if engine == nil {
		return tc
	}
	if cmd, ok := shellCommandForPolicy(tc); ok {
		callInfo := permission.BuildCallInfo(tc.Name, map[string]any{"command": cmd}, ws, home)
		if profile, matched := engine.SandboxFor(tc.Name, callInfo); matched {
			tc.Args = mergeSandboxArgs(tc.Args, profile)
		}
		return tc
	}
	if tc.Name != "shell" {
		return tc
	}
	callInfo := permission.BuildCallInfo(tc.Name, tc.Args, ws, home)
	if profile, matched := engine.SandboxFor(tc.Name, callInfo); matched {
		tc.Args = mergeSandboxArgs(tc.Args, profile)
	}
	return tc
}

func mergeSandboxArgs(args map[string]any, profile permission.SandboxProfile) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	next := make(map[string]any, len(args)+3)
	for k, v := range args {
		next[k] = v
	}
	if profile.SandboxMode != "" && !hasNonEmptyArg(next, "sandbox_mode") {
		next["sandbox_mode"] = profile.SandboxMode
	}
	// Rule-declared network is a default applied only when the caller did
	// not explicitly set one. If the LLM passed network=true/false, that
	// choice wins.
	if profile.Network {
		if _, ok := next["network"]; !ok {
			next["network"] = true
		}
	}
	if profile.OutboundDomains != nil {
		if raw, ok := next["outbound_domains"]; ok {
			if domains, err := netpolicy.Parse(raw); err == nil {
				next["outbound_domains"] = netpolicy.Intersect(domains, profile.OutboundDomains)
			}
		} else {
			next["outbound_domains"] = netpolicy.Intersect(nil, profile.OutboundDomains)
		}
	}
	if len(profile.WritableRoots) > 0 {
		if _, hasWritableRoots := next["writable_roots"]; !hasWritableRoots {
			next["writable_roots"] = append([]string(nil), profile.WritableRoots...)
		}
	}
	return next
}

func hasNonEmptyArg(args map[string]any, key string) bool {
	raw, ok := args[key]
	if !ok || raw == nil {
		return false
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	return true
}
