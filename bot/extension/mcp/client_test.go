package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekocode/bot/provider/types"
)

// startMockMCP builds and starts a minimal MCP server that responds to
// initialize, tools/list, and tools/call JSON-RPC methods.
func startMockMCP(t *testing.T, tools []toolDef) (*exec.Cmd, func()) {
	return startMockMCPWithDelay(t, tools, 0)
}

func startMockMCPWithDelay(t *testing.T, tools []toolDef, delay time.Duration) (*exec.Cmd, func()) {
	t.Helper()

	dir := t.TempDir()
	script := filepath.Join(dir, "mock-server.go")
	toolsData, _ := json.Marshal(tools)

	code := fmt.Sprintf(`package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

var toolsJSON = []byte(%q)

func main() {
	time.Sleep(time.Duration(%d))
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		id, _ := req["id"].(float64)

		var resp map[string]any

		switch method {
		case "initialize":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]string{"name": "mock", "version": "1.0"},
				},
			}
		case "tools/list":
			var tl any
			json.Unmarshal(toolsJSON, &tl)
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{"tools": tl},
			}
		case "tools/call":
			var params map[string]any
			if raw, ok := req["params"]; ok {
				b, _ := json.Marshal(raw)
				json.Unmarshal(b, &params)
			}
			name, _ := params["name"].(string)
			text := "ok: " + name
			if name == "cwd" {
				text, _ = os.Getwd()
			}
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"content": []map[string]string{{"type": "text", "text": text}},
					"isError": false,
				},
			}
		default:
			continue
		}
		out, _ := json.Marshal(resp)
		fmt.Println(string(out))
	}
}
`, toolsData, delay)

	os.WriteFile(script, []byte(code), 0o644)

	outPath := filepath.Join(dir, "mock-server")
	build := exec.Command("go", "build", "-o", outPath, script)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build mock server: %v\n%s", err, out)
	}

	cmd := exec.Command(outPath)
	cmd.Stderr = os.Stderr
	return cmd, func() {}
}

func TestClientInitialize(t *testing.T) {
	mockTools := []toolDef{
		{
			Name:        "mock-tool",
			Description: "A mock tool",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]types.Property{
					"input": {Type: "string", Description: "input param"},
				},
			},
		},
	}

	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Start()

	c := &client{
		name:   "test-mcp",
		config: ServerConfig{Command: cmd.Path},
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}

	if err := c.initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if tools[0].Name != "mock-tool" {
		t.Errorf("tool name = %q, want mock-tool", tools[0].Name)
	}

	result, err := c.CallTool(context.Background(), "mock-tool", map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result != "ok: mock-tool" {
		t.Errorf("result = %q, want 'ok: mock-tool'", result)
	}

	c.Close()
}

func TestClientStartStop(t *testing.T) {
	mockTools := []toolDef{
		{Name: "test", Description: "test tool", InputSchema: inputSchema{Type: "object"}},
	}
	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()

	c := newClient("test", ServerConfig{Command: cmd.Path})
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if c.cmd == nil || c.cmd.Process == nil {
		t.Error("should be alive after start")
	}

	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("tools len = %d, want 1", len(tools))
	}

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if c.cmd != nil {
		t.Error("should not be alive after close (cmd should be nil)")
	}
}

func TestClientDoubleStart(t *testing.T) {
	mockTools := []toolDef{
		{Name: "test", Description: "test", InputSchema: inputSchema{Type: "object"}},
	}
	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()

	c := newClient("test", ServerConfig{Command: cmd.Path})
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if c.cmd == nil || c.cmd.Process == nil {
		t.Error("should still be alive")
	}
	c.Close()
}

func TestClientCallToolNotStarted(t *testing.T) {
	c := newClient("offline", ServerConfig{Command: "/nonexistent"})
	_, err := c.CallTool(context.Background(), "test", nil)
	if err == nil {
		t.Error("should fail when server cannot start")
	}
}

func TestClientListToolsCancellation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hang-list.go")
	code := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		id, _ := req["id"].(float64)
		if method == "initialize" {
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id": id,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]string{"name": "hang-list", "version": "1.0"},
				},
			}
			out, _ := json.Marshal(resp)
			fmt.Println(string(out))
			continue
		}
		if method == "tools/list" {
			time.Sleep(10 * time.Second)
		}
	}
}
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatalf("write mock server: %v", err)
	}

	outPath := filepath.Join(dir, "hang-list")
	build := exec.Command("go", "build", "-o", outPath, script)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build mock server: %v\n%s", err, out)
	}

	c := newClient("hang-list", ServerConfig{Command: outPath})
	c.requestTimeout = 10 * time.Second
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.ListTools(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ListTools error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ListTools took too long: %s", elapsed)
	}
	if c.cmd != nil {
		t.Fatal("cancelled MCP process should be cleared")
	}
}

func TestClientCallToolCancellationWhileServerDoesNotRead(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "blocked-stdin.go")
	code := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return
	}
	var req map[string]any
	if json.Unmarshal(scanner.Bytes(), &req) != nil {
		return
	}
	id, _ := req["id"].(float64)
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id": id,
		"result": map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{"name": "blocked-stdin", "version": "1.0"},
		},
	}
	out, _ := json.Marshal(resp)
	fmt.Println(string(out))

	// Consume the initialized notification, then stop reading stdin.
	if scanner.Scan() {
		time.Sleep(10 * time.Minute)
	}
}
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatalf("write mock server: %v", err)
	}
	outPath := filepath.Join(dir, "blocked-stdin")
	build := exec.Command("go", "build", "-o", outPath, script)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build mock server: %v\n%s", err, out)
	}

	c := newClient("blocked-stdin", ServerConfig{Command: outPath})
	c.requestTimeout = 10 * time.Second
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.CallTool(ctx, "blocked", map[string]any{
		"input": strings.Repeat("x", 1<<20),
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallTool error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("CallTool took too long: %s", elapsed)
	}
	if c.cmd != nil {
		t.Fatal("cancelled MCP process should be cleared")
	}
}
