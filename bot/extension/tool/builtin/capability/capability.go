// Package capability implements the constant-schema proxy tool through which
// the model reaches MCP server tools. The provider-visible schema never
// changes when servers come and go — that keeps the prompt prefix
// byte-stable for the provider's prefix cache, which per-tool registration
// would break on every MCP add/remove.
package capability

import (
	"context"
	"fmt"
	"strings"

	"nekocode/bot/extension/mcp"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
)

// ToolName is the single, constant tool name through which the model
// reaches every MCP server tool.
const ToolName = "capability"

type tool struct {
	mcp *mcp.Manager
}

// New creates the capability proxy tool bound to the MCP runtime it adapts.
func New(manager *mcp.Manager) core.Tool {
	return &tool{mcp: manager}
}

// ResolveTarget translates one delegated call to the concrete MCP tool name
// used by permission rules, audit records, and confirmation UI.
func ResolveTarget(args map[string]any) (tools.CallTarget, bool) {
	if action, _ := args["action"].(string); action != "call" {
		return tools.CallTarget{}, false
	}
	server, _ := args["server"].(string)
	toolName, _ := args["tool"].(string)
	if server == "" || toolName == "" {
		return tools.CallTarget{}, false
	}
	callArgs, _ := args["args"].(map[string]any)
	return tools.CallTarget{Name: canonicalTargetName(server, toolName), Args: callArgs}, true
}

func canonicalTargetName(server, toolName string) string {
	return "mcp__" + escapeTargetPart(server) + "__" + escapeTargetPart(toolName)
}

func escapeTargetPart(value string) string {
	value = strings.ReplaceAll(value, "%", "%25")
	return strings.ReplaceAll(value, "__", "%5F%5F")
}

func (t *tool) Name() string { return ToolName }

func (t *tool) Description() string {
	return "Access MCP (Model Context Protocol) server tools through one stable entry point. " +
		"When a task might involve external services, databases, internal systems, or any capability " +
		"beyond the built-in tools, start with action=list to discover what is available — do not " +
		"assume an integration is missing before listing. " +
		"action=inspect shows one tool's full description and parameter schema; action=call invokes a tool with args."
}

func (t *tool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "action", Type: "string", Required: true, Enum: []string{"list", "inspect", "call"},
			Description: "list: show servers and tools; inspect: show one tool's schema; call: invoke a tool."},
		{Name: "server", Type: "string", Required: false,
			Description: "Server name from the list output; required for inspect and call."},
		{Name: "tool", Type: "string", Required: false,
			Description: "Tool name from the list output; required for inspect and call."},
		{Name: "args", Type: "object", Required: false,
			Description: "Arguments for the tool being called (see inspect for its schema)."},
	}
}

func (t *tool) ExecutionMode(args map[string]any) core.ExecutionMode {
	return core.ModeSequential
}

func (t *tool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	action, _ := args["action"].(string)
	server, _ := args["server"].(string)
	toolName, _ := args["tool"].(string)
	switch action {
	case "list":
		return core.ToolOutput{Text: t.mcp.ListCapabilities()}, nil
	case "inspect":
		if server == "" || toolName == "" {
			return core.ToolOutput{}, fmt.Errorf("inspect requires server and tool")
		}
		toolText, toolErr := t.mcp.InspectTool(server, toolName)
		return core.ToolOutput{Text: toolText}, toolErr
	case "call":
		if server == "" || toolName == "" {
			return core.ToolOutput{}, fmt.Errorf("call requires server and tool")
		}
		callArgs, _ := args["args"].(map[string]any)
		toolText, toolErr := t.mcp.CallServerTool(ctx, server, toolName, callArgs)
		return core.ToolOutput{Text: toolText}, toolErr
	}
	return core.ToolOutput{}, fmt.Errorf("unknown action %q: must be list, inspect, or call", action)
}
