package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"nekocode/bot/llm/types"
)

// startMockMCP builds and starts a minimal MCP server that responds to
// initialize, tools/list, and tools/call JSON-RPC methods.
func startMockMCP(t *testing.T, tools []ToolDef) (*exec.Cmd, func()) {
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
)

var toolsJSON = []byte(%q)

func main() {
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
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"content": []map[string]string{{"type": "text", "text": "ok: " + name}},
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
`, toolsData)

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
	mockTools := []ToolDef{
		{
			Name:        "mock-tool",
			Description: "A mock tool",
			InputSchema: InputSchema{
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

	c := &Client{
		Name:   "test-mcp",
		Config: ServerConfig{Command: cmd.Path},
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}

	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if tools[0].Name != "mock-tool" {
		t.Errorf("tool name = %q, want mock-tool", tools[0].Name)
	}

	result, err := c.CallTool("mock-tool", map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result != "ok: mock-tool" {
		t.Errorf("result = %q, want 'ok: mock-tool'", result)
	}

	c.Close()
}

func TestClientStartStop(t *testing.T) {
	mockTools := []ToolDef{
		{Name: "test", Description: "test tool", InputSchema: InputSchema{Type: "object"}},
	}
	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()

	c := NewClient("test", ServerConfig{Command: cmd.Path})
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if c.cmd == nil || c.cmd.Process == nil {
		t.Error("should be alive after start")
	}

	tools, err := c.ListTools()
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
	mockTools := []ToolDef{
		{Name: "test", Description: "test", InputSchema: InputSchema{Type: "object"}},
	}
	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()

	c := NewClient("test", ServerConfig{Command: cmd.Path})
	if err := c.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if c.cmd == nil || c.cmd.Process == nil {
		t.Error("should still be alive")
	}
	c.Close()
}

func TestClientCallToolNotStarted(t *testing.T) {
	c := NewClient("offline", ServerConfig{Command: "/nonexistent"})
	_, err := c.CallTool("test", nil)
	if err == nil {
		t.Error("should fail when server cannot start")
	}
}
