package capability

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/extension/mcp"
)

// TestSchemaIsStable pins the proxy's provider-visible schema: it must
// never change with MCP inventory, or the prompt prefix cache breaks on
// every add/remove.
func TestSchemaIsStable(t *testing.T) {
	tool := New(mcp.New())
	if tool.Name() != ToolName {
		t.Fatalf("name = %q, want %q", tool.Name(), ToolName)
	}
	params := tool.Parameters()
	if len(params) != 4 {
		t.Fatalf("params = %d, want 4 (action/server/tool/args)", len(params))
	}
	names := []string{params[0].Name, params[1].Name, params[2].Name, params[3].Name}
	want := []string{"action", "server", "tool", "args"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("param %d = %q, want %q", i, names[i], want[i])
		}
	}
	if !strings.Contains(tool.Description(), "action=list") {
		t.Fatal("description should teach the list-first discovery flow")
	}
}

func TestResolveTarget(t *testing.T) {
	target, ok := ResolveTarget(map[string]any{
		"action": "call", "server": "fs", "tool": "read", "args": map[string]any{"path": "/x"},
	})
	if !ok || target.Name != "mcp__fs__read" || target.Args["path"] != "/x" {
		t.Fatalf("target = %+v, %v", target, ok)
	}
	for _, args := range []map[string]any{
		{"action": "list"},
		{"action": "call", "server": "fs"},
		{"action": "call", "tool": "read"},
	} {
		if _, ok := ResolveTarget(args); ok {
			t.Fatalf("args %v must not resolve", args)
		}
	}
}

func TestResolveTargetEscapesAmbiguousSeparators(t *testing.T) {
	left, ok := ResolveTarget(map[string]any{"action": "call", "server": "a__b", "tool": "c"})
	if !ok {
		t.Fatal("left target did not resolve")
	}
	right, ok := ResolveTarget(map[string]any{"action": "call", "server": "a", "tool": "b__c"})
	if !ok {
		t.Fatal("right target did not resolve")
	}
	if left.Name == right.Name {
		t.Fatalf("ambiguous targets collided at %q", left.Name)
	}
	if left.Name != "mcp__a%5F%5Fb__c" || right.Name != "mcp__a__b%5F%5Fc" {
		t.Fatalf("escaped targets = %q and %q", left.Name, right.Name)
	}
}

func TestExecuteBadUsage(t *testing.T) {
	tool := New(mcp.New())
	ctx := context.Background()
	out, err := tool.Execute(ctx, map[string]any{"action": "list"})
	if err != nil || out.Render() != "No MCP servers configured." {
		t.Fatalf("empty list = %q, %v", out.Render(), err)
	}
	for _, args := range []map[string]any{
		{"action": "inspect"},                 // missing server/tool
		{"action": "inspect", "server": "fs"}, // missing tool
		{"action": "call"},                    // missing server/tool
		{"action": "call", "server": "fs"},    // missing tool
		{"action": "bogus"},                   // unknown action
	} {
		if _, err := tool.Execute(ctx, args); err == nil {
			t.Fatalf("args %v must error", args)
		}
	}
}
