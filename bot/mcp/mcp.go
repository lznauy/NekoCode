package mcp

import (
	ext "nekocode/bot/extension/mcp"
	"nekocode/common"
)

type ServerConfig = ext.ServerConfig
type ToolDef = ext.ToolDef
type InputSchema = ext.InputSchema
type Client = ext.Client
type MCPTool = ext.MCPTool

func NewClient(name string, cfg ServerConfig) *Client { return ext.NewClient(name, cfg) }

func NewMCPTool(client *Client, def ToolDef, level common.DangerLevel) *MCPTool {
	return ext.NewMCPTool(client, def, level)
}

func ParseDangerLevel(s string) common.DangerLevel { return ext.ParseDangerLevel(s) }
