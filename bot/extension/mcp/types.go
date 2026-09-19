package mcp

import (
	"encoding/json"

	"nekocode/bot/provider/types"
)

// ServerConfig defines how to launch an MCP server.
type ServerConfig struct {
	CWD     string            `json:"cwd,omitempty"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Registration binds a lifecycle owner ID and user-visible name to one MCP
// server configuration. Replace uses registrations as an atomic set.
type Registration struct {
	ID     string
	Name   string
	Config ServerConfig
}

// toolDef represents a tool discovered from an MCP server.
type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema inputSchema `json:"inputSchema"`
}

// inputSchema is JSON Schema for tool parameters.
type inputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]types.Property `json:"properties,omitempty"`
	Required   []string                  `json:"required,omitempty"`
}

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
