package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/filesystem/read"
)

func agentFixture(t *testing.T, root, owner, files string) string {
	t.Helper()
	dir := filepath.Join(root, ".nekocode", "plugins", owner)
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"`+owner+`","agents":[`+files+`]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "review.md"), []byte("---\nname: reviewer\ndescription: Review changes\ntools: [Read]\nskills: [check]\nmax_steps: 4\n---\nReview carefully."), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newAgentManager() *Manager {
	return New(Config{Context: ctxmgr.New(ctxmgr.Config{}), Tools: tools.New(&read.ReadTool{})})
}

func TestAgentSkillToolAvailableAcrossStartupAndReload(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	dir := agentFixture(t, root, "demo", `"agents/review.md"`)
	if err := os.WriteFile(filepath.Join(dir, "agents", "review.md"), []byte("---\nname: reviewer\ntools: [skill]\n---\nReview carefully."), 0600); err != nil {
		t.Fatal(err)
	}
	// A fresh registry also represents the configuration-rebuild path.
	for range 2 {
		m := newAgentManager()
		t.Cleanup(m.Close)
		m.Load()
		for pass := range 2 {
			if pass > 0 {
				m.Reload()
			}
			if _, err := m.AgentProfile("demo/reviewer"); err != nil {
				t.Fatal(err)
			}
			entry, err := m.tools.Lookup("skill")
			if err != nil {
				t.Fatal(err)
			}
			out, err := entry.Tool.Execute(t.Context(), map[string]any{"name": "check"})
			if err != nil || !strings.Contains(out, "skill_content") {
				t.Fatalf("skill tool after load: %q, %v", out, err)
			}
		}
	}
}

func TestAgentOwnershipDiscoveryAndReload(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	agentFixture(t, root, "a", `"agents/review.md"`)
	agentFixture(t, root, "b", `"agents/review.md"`)
	m, other := newAgentManager(), newAgentManager()
	m.Load()
	other.Load()
	defer m.Close()
	defer other.Close()
	if _, err := m.AgentProfile("reviewer"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("alias: %v", err)
	}
	p, err := m.AgentProfile("a/reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if p.Tools[0] != "read" || p.Skills[0] != "check" || p.MaxSteps != 4 {
		t.Fatalf("unresolved profile: %+v", p)
	}
	p.Tools[0] = "shell"
	p, _ = m.AgentProfile("a/reviewer")
	if p.Tools[0] != "read" {
		t.Fatal("snapshot mutation leaked")
	}
	if err := m.SetPluginEnabled("a", false); err != nil {
		t.Fatal(err)
	}
	p, err = m.AgentProfile("reviewer")
	if err != nil || p.Name != "b/reviewer" {
		t.Fatalf("remaining profile: %+v %v", p, err)
	}
	if _, err = other.AgentProfile("a/reviewer"); err != nil {
		t.Fatal("manager disable crossed instance boundary", err)
	}
	m.Reload()
	if _, err = m.AgentProfile("a/reviewer"); err == nil {
		t.Fatal("reload lost disabled state")
	}
	m.Close()
	if _, err = other.AgentProfile("a/reviewer"); err != nil {
		t.Fatal("manager close crossed instance boundary", err)
	}
	entry, err := other.tools.Lookup("agent_profiles")
	if err != nil {
		t.Fatal(err)
	}
	text, err := entry.Tool.Execute(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Profiles []AgentInfo `json:"profiles"`
	}
	if err = json.Unmarshal([]byte(text), &catalog); err != nil || len(catalog.Profiles) != 4 {
		t.Fatalf("catalog: %s %v", text, err)
	}
	if strings.Contains(text, "Review carefully.") {
		t.Fatal("catalog exposed system prompt")
	}
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 10 {
				if _, err := other.AgentProfile("b/reviewer"); err != nil {
					t.Error(err)
				}
				if _, err := entry.Tool.Execute(t.Context(), nil); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	for range 3 {
		other.Reload()
	}
	readers.Wait()
	if _, err := other.AgentProfile("a/reviewer"); err == nil {
		t.Fatal("explicit reload did not adopt persisted disabled state")
	}
}

func TestAgentActivationFailureIsAtomicAndRetryable(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	dir := agentFixture(t, root, "broken", `"agents/review.md","agents/bad.md"`)
	skillDir := filepath.Join(dir, "skills", "dependent")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: dependent\ndescription: depends on the plugin\n---\nUse this plugin."), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agents", "bad.md")
	if err := os.WriteFile(path, []byte("---\nname: broken\ntools: [Typo]\n---\nInstructions."), 0600); err != nil {
		t.Fatal(err)
	}
	m := newAgentManager()
	m.Load()
	defer m.Close()
	if _, ok := m.Skill("dependent"); ok {
		t.Fatal("failed plugin published skills")
	}
	if _, err := m.AgentProfile("broken/reviewer"); err == nil {
		t.Fatal("partially activated plugin")
	}
	if m.Snapshot().AgentErrors["broken"] == "" || !strings.Contains(m.pluginInfo([]string{"broken"}), "unknown agent tool") {
		t.Fatal("load error hidden")
	}
	menu, ok := m.pluginMenu(t.Context(), &command.Command{Args: []string{"enable"}})
	if !ok || len(menu.Items) != 1 || menu.Items[0].Value != "/plugin enable broken" {
		t.Fatalf("failed plugin missing from retry menu: %+v", menu)
	}
	if !strings.Contains(menu.Items[0].Description, "failed") {
		t.Fatalf("missing failure state: %+v", menu.Items[0])
	}
	if err := os.WriteFile(path, []byte("---\nname: other\ntools: [Read]\n---\nInstructions."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPluginEnabled("broken", true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AgentProfile("broken/reviewer"); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot().AgentErrors) != 0 {
		t.Fatal("stale error after successful retry")
	}
	menu, _ = m.pluginMenu(t.Context(), &command.Command{Args: []string{"enable"}})
	if len(menu.Items) != 0 {
		t.Fatalf("healthy enabled plugin remained in retry menu: %+v", menu)
	}
	if _, ok := m.Skill("dependent"); !ok {
		t.Fatal("successful retry did not publish skills")
	}
	if err := os.WriteFile(path, []byte("---\nname: reviewer\n---\nDuplicate."), 0600); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if _, ok := m.Skill("dependent"); ok {
		t.Fatal("failed reload retained skills")
	}
	if _, err := m.AgentProfile("broken/reviewer"); err == nil {
		t.Fatal("duplicate partially published")
	}
	if !strings.Contains(m.Snapshot().AgentErrors["broken"], "duplicate") {
		t.Fatal("missing duplicate diagnostic")
	}
	if err := m.SetPluginEnabled("broken", true); err == nil {
		t.Fatal("enabled invalid plugin")
	}
	if out := m.uninstallPlugin(t.Context(), []string{"broken"}); !strings.Contains(out, "Uninstalled") {
		t.Fatal(out)
	}
	if len(m.Snapshot().AgentErrors) != 0 {
		t.Fatal("uninstall retained failed activation diagnostic")
	}
}

func TestFailedAgentInstallRollsBackCatalog(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	dir := agentFixture(t, filepath.Join(root, "source"), "bad", `"agents/missing.md"`)
	m := newAgentManager()
	m.Load()
	defer m.Close()
	if out := m.install(t.Context(), dir); !strings.Contains(out, "Install cancelled") {
		t.Fatalf("install: %s", out)
	}
	if state := m.Snapshot(); len(state.AgentErrors) != 0 || len(state.Plugins) != 0 || len(state.Agents) != 2 {
		t.Fatalf("rollback left runtime state: %+v", state)
	}
}
