package mcp

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/llm/types"
	"nekocode/common"
)

func TestMCPToolAdapter(t *testing.T) {
	mockTools := []ToolDef{
		{
			Name:        "search-files",
			Description: "Search for files matching a pattern",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]types.Property{
					"pattern": {Type: "string", Description: "Glob pattern to match"},
					"dir":     {Type: "string", Description: "Directory to search"},
				},
				Required: []string{"pattern"},
			},
		},
	}
	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()

	c := NewClient("search-mcp", ServerConfig{Command: cmd.Path})
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()

	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	mt := NewMCPTool(c, tools[0], common.LevelWrite)

	if !strings.HasPrefix(mt.Name(), "search-mcp__") {
		t.Errorf("Name = %q, should have prefix search-mcp__", mt.Name())
	}
	if !strings.Contains(mt.Description(), "[MCP:search-mcp]") {
		t.Errorf("Description = %q, should contain [MCP:search-mcp]", mt.Description())
	}

	params := mt.Parameters()
	if len(params) != 2 {
		t.Fatalf("params len = %d, want 2", len(params))
	}
	paramMap := make(map[string]bool)
	for _, p := range params {
		paramMap[p.Name] = p.Required
	}
	if !paramMap["pattern"] {
		t.Error("pattern should be required")
	}
	if paramMap["dir"] {
		t.Error("dir should not be required")
	}
}

func TestMCPToolExecute(t *testing.T) {
	mockTools := []ToolDef{
		{Name: "echo", Description: "Echo back", InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]types.Property{"msg": {Type: "string"}},
		}},
	}
	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()

	c := NewClient("echo-mcp", ServerConfig{Command: cmd.Path})
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()

	tools, _ := c.ListTools()
	mt := NewMCPTool(c, tools[0], common.LevelWrite)

	result, err := mt.Execute(context.Background(), map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "ok: echo" {
		t.Errorf("result = %q, want 'ok: echo'", result)
	}
}

func TestParseDangerLevel(t *testing.T) {
	tests := []struct {
		input string
		want  common.DangerLevel
	}{
		{"safe", common.LevelSafe},
		{"SAFE", common.LevelSafe},
		{"Safe", common.LevelSafe},
		{"modify", common.LevelWrite},
		{"write", common.LevelWrite},
		{"danger", common.LevelDestructive},
		{"destructive", common.LevelDestructive},
		{"blocked", common.LevelForbidden},
		{"forbidden", common.LevelForbidden},
		{"", common.LevelWrite},        // unrecognized defaults to Write
		{"unknown", common.LevelWrite}, // unrecognized defaults to Write
	}
	for _, tt := range tests {
		got := ParseDangerLevel(tt.input)
		if got != tt.want {
			t.Errorf("ParseDangerLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestMCPToolDangerLevel(t *testing.T) {
	mockTools := []ToolDef{
		{Name: "test", Description: "test", InputSchema: InputSchema{Type: "object"}},
	}
	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()

	c := NewClient("test-mcp", ServerConfig{Command: cmd.Path})
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()

	tools, _ := c.ListTools()

	// Test each danger level is properly stored and returned.
	cases := []common.DangerLevel{common.LevelSafe, common.LevelWrite, common.LevelDestructive}
	for _, want := range cases {
		mt := NewMCPTool(c, tools[0], want)
		got := mt.DangerLevel(nil)
		if got != want {
			t.Errorf("DangerLevel(%v) = %v, want %v", want, got, want)
		}
	}
}
