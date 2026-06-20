package mcp

import (
	"context"
	"fmt"
	"strings"

	"nekocode/bot/tools"
	"nekocode/common"
)

// MCPTool adapts an MCP server tool to the tools.Tool interface.
type MCPTool struct {
	client      *Client
	def         ToolDef
	fullName    string
	dangerLevel common.DangerLevel
}

// NewMCPTool creates a tool adapter.
func NewMCPTool(client *Client, def ToolDef, level common.DangerLevel) *MCPTool {
	return &MCPTool{
		client:      client,
		def:         def,
		fullName:    client.Name + "__" + def.Name,
		dangerLevel: level,
	}
}

func (t *MCPTool) Name() string { return t.fullName }
func (t *MCPTool) Description() string {
	return fmt.Sprintf("[MCP:%s] %s", t.client.Name, t.def.Description)
}

func (t *MCPTool) Parameters() []tools.Parameter {
	var params []tools.Parameter
	for name, prop := range t.def.InputSchema.Properties {
		p := tools.Parameter{
			Name:        name,
			Type:        prop.Type,
			Description: prop.Description,
			Required:    isRequired(t.def.InputSchema.Required, name),
		}
		params = append(params, p)
	}
	return params
}

func (t *MCPTool) ExecutionMode(args map[string]any) tools.ExecutionMode {
	return tools.ModeSequential
}

func (t *MCPTool) DangerLevel(args map[string]any) common.DangerLevel {
	return t.dangerLevel
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	return t.client.CallTool(t.def.Name, args)
}

// ParseDangerLevel converts a string to a DangerLevel.
// Defaults to LevelWrite for unrecognized values.
func ParseDangerLevel(s string) common.DangerLevel {
	switch strings.ToLower(s) {
	case "safe":
		return common.LevelSafe
	case "modify", "write":
		return common.LevelWrite
	case "danger", "destructive":
		return common.LevelDestructive
	case "blocked", "forbidden":
		return common.LevelForbidden
	default:
		return common.LevelWrite
	}
}

func isRequired(required []string, name string) bool {
	for _, r := range required {
		if r == name {
			return true
		}
	}
	return false
}
