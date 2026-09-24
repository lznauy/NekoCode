package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// defaultRequestTimeout bounds a single JSON-RPC round trip.
const defaultRequestTimeout = 15 * time.Second

// client is a connection handle to one MCP server process: it owns the child
// process and the stdio pipes that JSON-RPC messages flow through. The wire
// protocol itself lives in protocol.go.
type client struct {
	remote *remoteClient
	name   string
	config ServerConfig

	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	stdoutPipe io.ReadCloser

	mu             sync.Mutex // serializes requests; guards the fields above
	reqID          atomic.Int64
	tools          []toolDef
	requestTimeout time.Duration
}

// newClient creates an unstarted client.
func newClient(name string, cfg ServerConfig) *client {
	c := &client{
		name:           name,
		config:         cfg,
		requestTimeout: defaultRequestTimeout,
	}
	if cfg.URL != "" {
		c.remote = newRemoteClient(name, cfg)
	}
	// Authorization options belong to this connection attempt, not to the
	// saved definition used by cancel/logout or subsequent reconnects.
	c.config.interactive = false
	c.config.authorizationScopes = nil
	return c
}

// Start launches the MCP server process and performs the initialize
// handshake. It is idempotent: an already-running client is a no-op.
func (c *client) Start(ctx context.Context) error {
	if c.remote != nil {
		return c.remote.Start(ctx)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cmd := exec.Command(c.config.Command, c.config.Args...)
	cmd.Dir = c.config.CWD
	configureProcess(cmd)
	cmd.Env = os.Environ()
	for k, v := range c.config.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start server: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	c.stdoutPipe = stdout

	if err := c.initialize(ctx); err != nil {
		_ = c.stopLocked(2 * time.Second)
		return fmt.Errorf("initialize: %w", err)
	}
	return nil
}

// Close stops the MCP server process.
func (c *client) Close() error {
	if c.remote != nil {
		return c.remote.Close()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopLocked(2 * time.Second)
}

// stopLocked terminates the server process immediately: stdin is closed and
// the whole process group is killed without a graceful-exit window. MCP
// commands are untrusted session input and may spawn descendants that inherit
// stdio and outlive the direct child. The timeout only guards the
// pathological case where the tree kill failed; the wait then falls back to
// killing the direct child. The caller must hold c.mu.
func (c *client) stopLocked(timeout time.Duration) error {
	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}

	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	_ = killProcessTree(c.cmd.Process)
	if c.stdoutPipe != nil {
		_ = c.stdoutPipe.Close()
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- c.cmd.Wait() }()

	select {
	case <-waitCh:
	case <-time.After(timeout):
		_ = c.cmd.Process.Kill()
		<-waitCh
	}

	c.cmd, c.stdin, c.stdout, c.stdoutPipe = nil, nil, nil, nil
	return nil
}

// ListTools discovers the server's tools, starting it if necessary.
func (c *client) ListTools(ctx context.Context) ([]toolDef, error) {
	if c.remote != nil {
		return c.remote.ListTools(ctx)
	}
	if err := c.Start(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var resp struct {
		Tools []toolDef `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}

	c.tools = resp.Tools
	return c.tools, nil
}

// CallTool invokes a tool on the server, starting it if necessary.
func (c *client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if c.remote != nil {
		return c.remote.CallTool(ctx, name, args)
	}
	if err := c.Start(ctx); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.sendRequest(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", fmt.Errorf("tools/call %s: %w", name, err)
	}
	return parseToolCallResult(result)
}

// parseToolCallResult extracts the text content of a tools/call result,
// folding a protocol-level tool error into the returned error.
func parseToolCallResult(result []byte) (string, error) {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse tools/call: %w", err)
	}

	var texts []string
	for _, item := range resp.Content {
		if item.Type == "text" {
			texts = append(texts, item.Text)
		}
	}
	text := strings.Join(texts, "")
	if resp.IsError {
		return text, fmt.Errorf("tool error: %s", text)
	}
	return text, nil
}

func (c *client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "NekoCode",
			"version": "0.1.0",
		},
	}

	result, err := c.sendRequest(ctx, "initialize", params)
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

	if err := c.sendNotification(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("send initialized: %w", err)
	}
	return nil
}

func (c *client) sendRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := c.reqID.Add(1)
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

	timeout := c.timeout()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	if err := c.writeMessage(ctx, timer.C, method, timeout, body); err != nil {
		return nil, err
	}

	type readResult struct {
		line []byte
		err  error
	}
	reader := c.stdout
	if reader == nil {
		return nil, fmt.Errorf("read: MCP server stdout unavailable")
	}
	readCh := make(chan readResult, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		readCh <- readResult{line: line, err: err}
	}()

	var line []byte
	select {
	case result := <-readCh:
		if result.err != nil {
			return nil, fmt.Errorf("read: %w", result.err)
		}
		line = result.line
	case <-ctx.Done():
		_ = c.stopLocked(100 * time.Millisecond)
		return nil, ctx.Err()
	case <-timer.C:
		_ = c.stopLocked(100 * time.Millisecond)
		return nil, fmt.Errorf("%s timed out after %s", method, timeout)
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

func (c *client) sendNotification(ctx context.Context, method string, params any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	notif := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	timeout := c.timeout()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	return c.writeMessage(ctx, timer.C, method, timeout, body)
}

func (c *client) timeout() time.Duration {
	if c.requestTimeout > 0 {
		return c.requestTimeout
	}
	return defaultRequestTimeout
}

func (c *client) writeMessage(
	ctx context.Context,
	timeout <-chan time.Time,
	method string,
	timeoutDuration time.Duration,
	body []byte,
) error {
	writer := c.stdin
	if writer == nil {
		return fmt.Errorf("write: MCP server stdin unavailable")
	}
	writeCh := make(chan error, 1)
	go func() {
		_, err := writer.Write(append(body, '\n'))
		writeCh <- err
	}()

	select {
	case err := <-writeCh:
		if err != nil {
			return fmt.Errorf("write: %w", err)
		}
		return nil
	case <-ctx.Done():
		_ = c.stopLocked(100 * time.Millisecond)
		return ctx.Err()
	case <-timeout:
		_ = c.stopLocked(100 * time.Millisecond)
		return fmt.Errorf("%s timed out after %s", method, timeoutDuration)
	}
}
