package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/llm/types"
)

// ServerConfig defines how to launch an MCP server.
type ServerConfig struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	DangerLevel string            `json:"dangerLevel,omitempty"` // "safe", "modify", "danger"
}

// ToolDef represents a tool discovered from an MCP server.
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema is JSON Schema for tool parameters.
type InputSchema struct {
	Type       string                `json:"type"`
	Properties map[string]types.Property `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
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

	tools    []ToolDef
}

// NewClient creates an unstarted client.
func NewClient(name string, cfg ServerConfig) *Client {
	return &Client{
		Name:   name,
		Config: cfg,
	}
}

// Start launches the MCP server process and performs the initialize handshake.
func (c *Client) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil {
		return nil // already started
	}

	c.cmd = exec.Command(c.Config.Command, c.Config.Args...)
	// Start with parent environment so MCP servers inherit PATH, HOME, etc.
	c.cmd.Env = append(c.cmd.Env, os.Environ()...)
	for k, v := range c.Config.Env {
		c.cmd.Env = append(c.cmd.Env, k+"="+v)
	}

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReader(stdout)

	if err := c.cmd.Start(); err != nil {
		c.stdin.Close()
		return fmt.Errorf("start server: %w", err)
	}

	// Initialize handshake.
	if err := c.initialize(); err != nil {
		c.stdin.Close()
		_ = c.cmd.Process.Kill()
		c.cmd = nil
		return fmt.Errorf("initialize: %w", err)
	}

	return nil
}

// ListTools returns cached tools (call Start first).
func (c *Client) ListTools() ([]ToolDef, error) {
	if err := c.Start(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var listResp struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResp); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}

	c.tools = listResp.Tools
	return c.tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(name string, args map[string]any) (string, error) {
	if err := c.Start(); err != nil {
		return "", err
	}

	params := map[string]any{
		"name":      name,
		"arguments": args,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.sendRequest("tools/call", params)
	if err != nil {
		return "", fmt.Errorf("tools/call %s: %w", name, err)
	}

	var callResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &callResp); err != nil {
		return "", fmt.Errorf("parse tools/call: %w", err)
	}

	var text string
	for _, item := range callResp.Content {
		if item.Type == "text" {
			text += item.Text
		}
	}
	if callResp.IsError {
		return text, fmt.Errorf("tool error: %s", text)
	}
	return text, nil
}

// Close stops the MCP server.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}

	// Close stdin to signal the server to shut down.
	if c.stdin != nil {
		_ = c.stdin.Close()
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- c.cmd.Wait() }()

	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
		<-waitCh // drain
	}

	c.cmd = nil
	c.stdin = nil
	c.stdout = nil
	return nil
}

// --- JSON-RPC ----------------------------------------------------------------

type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  any `json:"params,omitempty"`
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
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  any `json:"params,omitempty"`
}

func (c *Client) initialize() error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "NekoCode",
			"version": "0.1.0",
		},
	}

	result, err := c.sendRequest("initialize", params)
	if err != nil {
		return err
	}

	var initResp struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &initResp); err != nil {
		return fmt.Errorf("parse initialize: %w", err)
	}

	if err := c.sendNotification("notifications/initialized", nil); err != nil {
		return fmt.Errorf("send initialized: %w", err)
	}
	return nil
}

func (c *Client) sendRequest(method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.reqID, 1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if _, err := fmt.Fprintf(c.stdin, "%s\n", body); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	// Read response line.
	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	if resp.ID != id {
		return nil, fmt.Errorf("response id mismatch: got %d, expected %d", resp.ID, id)
	}

	return resp.Result, nil
}

func (c *Client) sendNotification(method string, params any) error {
	notif := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.stdin, "%s\n", body)
	return err
}
