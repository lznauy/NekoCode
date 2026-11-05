package core

import (
	"encoding/json"
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/mcp"
)

func TestMCPInventoryRefreshesWithoutChangingStaticPrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	b := newPersistTestBot(t)
	stable := b.promptBuilder.BuildStatic()
	before := b.promptBuilder.BuildEnvironment()
	if err := b.ext.AddMCPServerBackground("inventory-fixture", mcp.ServerConfig{Command: "/nonexistent/nekocode-inventory-fixture"}); err != nil {
		t.Fatal(err)
	}
	after := b.promptBuilder.BuildEnvironment()
	if !strings.Contains(after, "inventory-fixture") || after == before {
		t.Fatalf("current MCP inventory missing from runtime context: %s", after)
	}
	request, err := json.Marshal(b.ctxMgr.BuildRequest(ctxmgr.ModelRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), "inventory-fixture") {
		t.Fatal("MCP inventory did not reach provider request")
	}
	if strings.Contains(after, "/nonexistent/") {
		t.Fatal("startup command leaked into model environment")
	}
	if b.promptBuilder.BuildStatic() != stable {
		t.Fatal("MCP inventory rewrote cached static prefix")
	}
	if strings.Contains(b.ctxMgr.Snapshot().SystemPrompt, "inventory-fixture") {
		t.Fatal("inventory persisted in session prompt")
	}
	b.ext.Close()
	if current := b.promptBuilder.BuildEnvironment(); strings.Contains(current, "inventory-fixture") {
		t.Fatalf("stale MCP retained: %s", current)
	}
}
