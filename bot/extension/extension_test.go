package extension

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"nekocode/bot/command"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/tool"
	"nekocode/bot/policy"
)

func TestManagerOwnsPluginSkillLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))

	pluginDir := filepath.Join(root, ".nekocode", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"demo plugin","skills":["skills/demo"]}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pluginDir, "skills", "demo", "SKILL.md"),
		[]byte("---\nname: demo-skill\ndescription: demo\n---\nDemo body.\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	registry := tools.New()
	contextManager := ctxmgr.New(ctxmgr.Config{})
	manager := New(Config{
		Context: contextManager, Tools: registry,
		Policy: policy.New(), ContextWindow: 32_000,
	})
	manager.Load()
	defer manager.Close()
	commands := command.New(command.Deps{
		CtxMgr: contextManager, ToolRegistry: registry,
		GetConfigFn: func() config.ModelConfig { return config.ModelConfig{} },
	})
	manager.RegisterCommands(commands, nil)
	menu, ok := commands.Menu(context.Background(), "/plugin")
	if !ok || menu.Title != "Plugin action" || len(menu.Items) != 6 {
		t.Fatalf("plugin action menu = %+v, %v", menu, ok)
	}
	menu, ok = commands.Menu(context.Background(), "/plugin disable")
	if !ok || len(menu.Items) != 1 || menu.Items[0].Value != "/plugin disable demo plugin" {
		t.Fatalf("plugin choice menu = %+v, %v", menu, ok)
	}
	if result, handled := commands.Execute(context.Background(), menu.Items[0].Value, contextManager); !handled || !strings.Contains(result, `Disabled plugin "demo plugin"`) {
		t.Fatalf("spaced plugin command = %q, %v", result, handled)
	}
	if result, handled := commands.Execute(context.Background(), "/plugin enable demo plugin", contextManager); !handled || !strings.Contains(result, `Enabled plugin "demo plugin"`) {
		t.Fatalf("spaced plugin re-enable = %q, %v", result, handled)
	}

	if got := manager.Snapshot(); len(got.Plugins) != 1 {
		t.Fatalf("plugins = %d, want 1", len(got.Plugins))
	}
	if _, ok := manager.Skill("demo-skill"); !ok {
		t.Fatal("plugin skill was not exposed through the extension entry point")
	}
	if !slices.Contains(commands.Names(), "$demo-skill") {
		t.Fatal("plugin skill command was not registered")
	}
	if !registry.Has("skill") {
		t.Fatal("skill tool was not registered")
	}

	if err := manager.SetPluginEnabled("demo plugin", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Skill("demo-skill"); ok {
		t.Fatal("disabled plugin skill is still exposed")
	}
	if slices.Contains(commands.Names(), "$demo-skill") {
		t.Fatal("disabled plugin skill command remained registered")
	}

	if err := manager.SetPluginEnabled("demo plugin", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Skill("demo-skill"); !ok {
		t.Fatal("re-enabled plugin skill was not restored")
	}
	if !slices.Contains(commands.Names(), "$demo-skill") {
		t.Fatal("re-enabled plugin skill command was not restored")
	}

	if err := os.RemoveAll(pluginDir); err != nil {
		t.Fatal(err)
	}
	manager.Reload()
	if got := manager.Snapshot(); len(got.Plugins) != 0 {
		t.Fatalf("reload retained a removed plugin: %+v", got.Plugins)
	}
	if _, ok := manager.Skill("demo-skill"); ok {
		t.Fatal("reload retained a removed plugin skill")
	}
	if slices.Contains(commands.Names(), "$demo-skill") {
		t.Fatal("reload retained a removed plugin skill command")
	}
}

// A published management snapshot must not alias plugin state that
// Enable/Disable mutate in place; otherwise viewmodel reads race with
// /plugin toggles.
func TestSnapshotPluginsDoNotAliasEnabledState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	pluginDir := filepath.Join(root, ".nekocode", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"demo"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	manager := New(Config{ProjectRoot: root})
	manager.Load()
	defer manager.Close()

	before := manager.Snapshot()
	if len(before.Plugins) != 1 || !before.Plugins[0].Enabled {
		t.Fatalf("unexpected initial plugins: %+v", before.Plugins)
	}
	if err := manager.SetPluginEnabled("demo", false); err != nil {
		t.Fatal(err)
	}
	if !before.Plugins[0].Enabled {
		t.Fatal("published snapshot aliased live Enabled state")
	}
	after := manager.Snapshot()
	if len(after.Plugins) != 1 || after.Plugins[0].Enabled {
		t.Fatalf("fresh snapshot did not observe disabled plugin: %+v", after.Plugins)
	}
}

func TestInstallReturnsFailure(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))

	manager := New(Config{
		Context: ctxmgr.New(ctxmgr.Config{}), Tools: tools.New(),
		Policy: policy.New(), ContextWindow: 32_000,
	})
	defer manager.Close()

	result := manager.installPlugin(context.Background(), []string{"./missing-plugin", "--yes"}, nil)
	if !strings.Contains(result, "Install failed") {
		t.Fatalf("result = %q", result)
	}
}

func TestSessionMCPServerNameCollision(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))

	manager := New(Config{
		Context: ctxmgr.New(ctxmgr.Config{}), Tools: tools.New(),
		Policy: policy.New(), ContextWindow: 32_000,
	})
	defer manager.Close()

	// Background host registrations claim their name synchronously even while
	// startup is still in progress.
	if err := manager.AddMCPServerBackground("github", mcp.ServerConfig{Command: "/nonexistent"}); err != nil {
		t.Fatal(err)
	}

	// A session-supplied server with the same name must be skipped instead
	// of failing session setup.
	if err := manager.ReplaceSessionMCPServers(context.Background(), "acp", map[string]mcp.ServerConfig{
		"github": {Command: "/nonexistent"},
	}); err != nil {
		t.Fatalf("session setup failed on name collision: %v", err)
	}
	if ids := manager.sessionMCP["acp"]; len(ids) != 0 {
		t.Fatalf("colliding server was registered: %v", ids)
	}

	// A distinct name still registers (and fails to start, proving the
	// registration was attempted rather than skipped).
	if err := manager.ReplaceSessionMCPServers(context.Background(), "acp", map[string]mcp.ServerConfig{
		"unique": {Command: "/nonexistent"},
	}); err == nil {
		t.Fatal("expected startup error for distinct server")
	}
	if owner := manager.mcp.Owner("unique"); owner != "" {
		t.Fatalf("failed session server retained owner %q", owner)
	}
	if ids := manager.sessionMCP["acp"]; len(ids) != 0 {
		t.Fatalf("failed replacement changed tracked IDs: %v", ids)
	}
}
