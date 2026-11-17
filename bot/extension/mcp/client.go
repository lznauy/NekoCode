package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/logger"
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
	killTree       func(*os.Process) error
	stopping       bool
	treeStopped    bool
	wait           *processWait
	writePending   <-chan error
	readPending    <-chan stdioReadResult
}

type processWait struct {
	done chan struct{}
	err  error // read only after done closes
}

type stdioReadResult struct {
	line []byte
	err  error
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

	if err := ctx.Err(); err != nil {
		return err
	}
	if c.stopping {
		if err := c.stopLocked(2 * time.Second); err != nil {
			return fmt.Errorf("previous MCP server cleanup incomplete: %w", err)
		}
	}
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
		return errors.Join(fmt.Errorf("initialize: %w", err), c.stopLocked(2*time.Second))
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
	err := c.stopLocked(2 * time.Second)
	if err != nil {
		// Manager shutdown/removal has a void API; it must still expose a
		// cleanup failure when its caller cannot receive the returned error.
		logger.Log("MCP %s cleanup failed: %v", c.name, err)
	}
	return err
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

// parseToolCallResult preserves structured data and non-text content as JSON
// alongside text. Resource locations are data, never grants to read them.
func parseToolCallResult(result []byte) (string, error) {
	var resp struct {
		Content           []json.RawMessage `json:"content"`
		StructuredContent json.RawMessage   `json:"structuredContent"`
		IsError           bool              `json:"isError"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse tools/call: %w", err)
	}
	var blocks []string
	for _, raw := range resp.Content {
		var item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return "", fmt.Errorf("parse tools/call content: %w", err)
		}
		if item.Type == "text" {
			blocks = append(blocks, item.Text)
		} else {
			blocks = append(blocks, "MCP content (JSON; binary media remains encoded):\n"+string(raw))
		}
	}
	if len(resp.StructuredContent) > 0 && string(resp.StructuredContent) != "null" {
		blocks = append(blocks, "Structured result (JSON):\n"+string(resp.StructuredContent))
	}
	text := strings.Join(blocks, "\n\n")
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

	reader := c.stdout
	if reader == nil {
		return nil, fmt.Errorf("read: MCP server stdout unavailable")
	}
	// Requests are serialized by c.mu. Read only while a request is pending,
	// discarding notifications instead of queuing them without a bound.
	skipped := 0
	var frames []json.RawMessage
	var response *jsonrpcResponse
	for {
		if skipped >= maxSkippedStdioFrames {
			return nil, errors.Join(fmt.Errorf("too many invalid or unrelated MCP messages"), c.stopLocked(100*time.Millisecond))
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), c.stopLocked(100*time.Millisecond))
		case <-timer.C:
			return nil, errors.Join(fmt.Errorf("%s timed out after %s", method, timeout), c.stopLocked(100*time.Millisecond))
		default:
		}
		if len(frames) == 0 && response != nil {
			if response.Error != nil {
				return nil, fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
			}
			return response.Result, nil
		}
		if len(frames) == 0 {
			readCh := make(chan stdioReadResult, 1)
			c.readPending = readCh
			go func() {
				line, err := readStdioFrame(reader)
				readCh <- stdioReadResult{line: line, err: err}
			}()

			var line []byte
			select {
			case result := <-readCh:
				c.readPending = nil
				if result.err != nil {
					return nil, errors.Join(fmt.Errorf("read: %w", result.err), c.stopLocked(100*time.Millisecond))
				}
				line = result.line
			case <-ctx.Done():
				cleanupErr := c.stopLocked(100 * time.Millisecond)
				return nil, errors.Join(ctx.Err(), cleanupErr)
			case <-timer.C:
				cleanupErr := c.stopLocked(100 * time.Millisecond)
				return nil, errors.Join(fmt.Errorf("%s timed out after %s", method, timeout), cleanupErr)
			}

			line = bytes.TrimSpace(line)
			if len(line) > 0 && line[0] == '[' {
				if err := json.Unmarshal(line, &frames); err != nil {
					frames = nil
				}
			} else {
				frames = []json.RawMessage{line}
			}
		}
		if len(frames) == 0 {
			skipped++
			continue
		}
		line := frames[0]
		frames = frames[1:]
		var message struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			ID      json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(line, &message); err != nil || message.JSONRPC != "2.0" {
			skipped++
			continue
		}
		if message.Method != "" {
			if len(message.ID) != 0 {
				// No server-initiated capabilities are advertised. Preserve its ID
				// (which may be a string) and explicitly reject unsupported requests.
				body, err := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "id": message.ID,
					"error": map[string]any{"code": -32601, "message": "Method not supported"},
				})
				if err != nil {
					return nil, fmt.Errorf("marshal: %w", err)
				}
				if err := c.writeMessage(ctx, timer.C, method, timeout, body); err != nil {
					return nil, err
				}
			}
			continue
		}
		var resp jsonrpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			skipped++
			continue
		}
		if resp.ID != id {
			skipped++
			continue
		}
		if resp.Result == nil && resp.Error == nil {
			skipped++
			continue
		}
		// Finish the already-read batch before returning: a server request
		// after our response still needs its reply, including string IDs.
		if response == nil {
			response = &resp
		}
	}
}

const (
	maxStdioFrameBytes    = 16 << 20
	maxSkippedStdioFrames = 64
)

// Bound a single line before JSON decoding; a broken server must not grow
// memory without limit. The request deadline still bounds noise recovery.
func readStdioFrame(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxStdioFrameBytes {
			return nil, fmt.Errorf("MCP message exceeds %d bytes", maxStdioFrameBytes)
		}
		line = append(line, fragment...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
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
	message := make([]byte, len(body)+1)
	copy(message, body)
	message[len(body)] = '\n'
	writeCh := make(chan error, 1)
	c.writePending = writeCh
	go func() {
		_, err := writer.Write(message)
		writeCh <- err
	}()

	select {
	case err := <-writeCh:
		c.writePending = nil
		if err != nil {
			return errors.Join(fmt.Errorf("write: %w", err), c.stopLocked(100*time.Millisecond))
		}
		return nil
	case <-ctx.Done():
		return errors.Join(ctx.Err(), c.stopLocked(100*time.Millisecond))
	case <-timeout:
		return errors.Join(fmt.Errorf("%s timed out after %s", method, timeoutDuration), c.stopLocked(100*time.Millisecond))
	}
}
