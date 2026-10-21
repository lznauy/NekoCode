package runner

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/capability"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

func capabilityCall(server, tool string, args map[string]any) core.ToolCallItem {
	return core.ToolCallItem{
		ID:   "1",
		Name: "capability",
		Args: map[string]any{"action": "call", "server": server, "tool": tool, "args": args},
	}
}

// A deny rule written for the real MCP tool (mcp__fs__read_file) must block the
// call even when the model invokes it through the capability proxy.
func TestCapabilityPermissionDenyUsesEffectiveTool(t *testing.T) {
	r := tools.New()
	r.RegisterWithOptions(capability.New(mcp.New()), tools.RegistrationOptions{ResolveTarget: capability.ResolveTarget})
	e := NewExecutor(r)
	e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"mcp__fs__read_file"}}, "/repo", "/home/user")

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{capabilityCall("fs", "read_file", nil)})[0]
	if !strings.Contains(got.Error, "denied") {
		t.Fatalf("deny rule did not block capability call: %+v", got)
	}
}

// An ask rule for the real tool prompts with the effective tool name (not
// the proxy name), so the user sees what actually runs.
func TestCapabilityPermissionAskShowsEffectiveTool(t *testing.T) {
	r := tools.New()
	r.RegisterWithOptions(capability.New(mcp.New()), tools.RegistrationOptions{ResolveTarget: capability.ResolveTarget})
	e := NewExecutor(r)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"mcp__fs__read_file"}}, "/repo", "/home/user")

	var promptedTool string
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		promptedTool = req.ToolName
		return protocol.ConfirmReply{Allowed: false}
	})
	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{capabilityCall("fs", "read_file", nil)})[0]
	if got.Error == "" {
		t.Fatalf("ask rule should require confirmation: %+v", got)
	}
	if promptedTool != "mcp__fs__read_file" {
		t.Fatalf("confirm prompt showed %q, want the effective tool mcp__fs__read_file", promptedTool)
	}
}

func TestCapabilityPermissionAllowUsesEffectiveTool(t *testing.T) {
	r := tools.New()
	r.RegisterWithOptions(&permissionProxyTool{}, tools.RegistrationOptions{ResolveTarget: capability.ResolveTarget})
	e := NewExecutor(r)
	e.SetPermissionPolicy(permission.PermissionsDecl{
		Allow: []string{"mcp__fs__read_file"},
		Ask:   []string{"capability"},
	}, "/repo", "/home/user")
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		t.Fatalf("effective allow unexpectedly prompted as %q", req.ToolName)
		return protocol.Deny()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{capabilityCall("fs", "read_file", nil)})[0]
	if got.Error != "" || !strings.Contains(got.Output, "called") {
		t.Fatalf("effective allow did not run delegated call: %+v", got)
	}
}

func TestCapabilityDefaultsToAskForUnknownRemoteTool(t *testing.T) {
	r := tools.New()
	r.RegisterWithOptions(capability.New(mcp.New()), tools.RegistrationOptions{ResolveTarget: capability.ResolveTarget})
	e := NewExecutor(r)

	var promptedTool string
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		promptedTool = req.ToolName
		return protocol.ConfirmReply{Allowed: false}
	})
	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{capabilityCall("github", "delete_repository", nil)})[0]
	if got.Error == "" {
		t.Fatalf("unknown remote capability ran without confirmation: %+v", got)
	}
	if promptedTool != "mcp__github__delete_repository" {
		t.Fatalf("prompted tool = %q, want effective remote target", promptedTool)
	}
}

type permissionProxyTool struct{}

func (*permissionProxyTool) Name() string                 { return capability.ToolName }
func (*permissionProxyTool) Description() string          { return "permission proxy test" }
func (*permissionProxyTool) Parameters() []core.Parameter { return nil }
func (*permissionProxyTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}
func (*permissionProxyTool) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "called"}, nil
}
