package extension

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/bot/command"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/tool"
	"nekocode/bot/policy"
)

func TestSessionMCPReplacementDoesNotBlockSnapshot(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		http.Error(w, "stopped", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	manager := New(Config{Context: ctxmgr.New(ctxmgr.Config{}), Tools: tools.New(), Policy: policy.New()})
	defer manager.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- manager.ReplaceSessionMCPServers(ctx, "test", map[string]mcp.ServerConfig{"slow": {URL: server.URL}})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		cancel()
		close(release)
		t.Fatal("remote startup did not begin")
	}
	snapshotDone := make(chan struct{})
	go func() { manager.Snapshot(); close(snapshotDone) }()
	select {
	case <-snapshotDone:
	case <-time.After(200 * time.Millisecond):
		cancel()
		close(release)
		t.Fatal("Snapshot blocked behind remote replacement startup")
	}
	cancel()
	close(release)
	<-result
}

func TestMCPLoginCommandDoesNotWaitForDiscovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	defer close(release)
	manager := New(Config{Context: ctxmgr.New(ctxmgr.Config{}), Tools: tools.New(), Policy: policy.New()})
	defer manager.Close()
	if err := manager.mcp.AddBackground("config:docs", "docs", mcp.ServerConfig{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	commands := command.New(command.Deps{})
	manager.RegisterCommands(commands, nil)
	done := make(chan struct{})
	go func() { commands.Execute(context.Background(), "/mcp-login docs", nil); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("login command blocked on discovery")
	}
}

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

func TestMCPStatusCommand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))

	registry := tools.New()
	contextManager := ctxmgr.New(ctxmgr.Config{})
	manager := New(Config{
		Context: contextManager, Tools: registry,
		Policy: policy.New(), ContextWindow: 32_000,
	})
	defer manager.Close()
	commands := command.New(command.Deps{
		CtxMgr: contextManager, ToolRegistry: registry,
		GetConfigFn: func() config.ModelConfig { return config.ModelConfig{} },
	})
	manager.RegisterCommands(commands, nil)

	if isCommand, duringTask := commands.CommandAvailability("/mcp"); !isCommand || !duringTask {
		t.Fatalf("/mcp availability = %v, %v; want command, during-task", isCommand, duringTask)
	}
	// OAuth actions must also stay usable while a task is running: they only
	// start or cancel a background browser flow.
	for _, action := range []string{"login", "logout", "cancel"} {
		if isCommand, duringTask := commands.CommandAvailability("/mcp-" + action); !isCommand || !duringTask {
			t.Fatalf("/mcp-%s availability = %v, %v; want command, during-task", action, isCommand, duringTask)
		}
	}

	// With no servers the command reports the empty state.
	if out, _ := commands.Execute(context.Background(), "/mcp", contextManager); !strings.Contains(out, "未连接任何 MCP 服务器") {
		t.Fatalf("empty /mcp output = %q", out)
	}

	// AddBackground claims the name and health entry synchronously, so the
	// listing includes the server immediately even before startup finishes.
	if err := manager.AddMCPServerBackground("github", mcp.ServerConfig{Command: "/nonexistent", Args: []string{"-m", "server"}}); err != nil {
		t.Fatal(err)
	}
	out, _ := commands.Execute(context.Background(), "/mcp", contextManager)
	if !strings.Contains(out, "github") || !strings.Contains(out, "配置") || !strings.Contains(out, "/nonexistent -m server") {
		t.Fatalf("/mcp output missing server details: %q", out)
	}

	// Owner labels cover plugin and session owners too.
	if got := mcpOwnerLabel("plugin:notes:fs"); got != "插件 notes" {
		t.Fatalf("plugin owner label = %q", got)
	}
	if got := mcpOwnerLabel("session:acp:docs"); got != "会话 acp" {
		t.Fatalf("session owner label = %q", got)
	}
}
