package mcp

import (
	"bufio"
	"io"
	"os/exec"
	"sync"

	"nekocode/bot/llm/types"
)

// ServerConfig defines how to launch an MCP server.
type ServerConfig struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	DangerLevel string            `json:"dangerLevel,omitempty"`
}

// ToolDef represents a tool discovered from an MCP server.
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema is JSON Schema for tool parameters.
type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]types.Property `json:"properties,omitempty"`
	Required   []string                  `json:"required,omitempty"`
}

// Client manages a connection to one MCP server process.
type Client struct {
	Name   string
	Config ServerConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	reqID  int64

	tools []ToolDef
}

// NewClient creates an unstarted client.
func NewClient(name string, cfg ServerConfig) *Client {
	return &Client{
		Name:   name,
		Config: cfg,
	}
}
