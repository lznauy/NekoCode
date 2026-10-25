package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/command"
	"nekocode/bot/policy"
)

func TestHookActivationFailureIsVisibleAtomicAndRetryable(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	dir := agentFixture(t, root, "broken", `"agents/review.md"`)
	manifest := `{"name":"broken","agents":["agents/review.md"],"hooks":{"source":"hooks.json"}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(hookPath, []byte(`{"Typo":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := newAgentManager()
	m.policy = policy.New()
	m.Load()
	defer m.Close()
	if m.Snapshot().HookErrors["broken"] == "" || !strings.Contains(m.pluginInfo([]string{"broken"}), "Hook load error") {
		t.Fatal("hook error hidden")
	}
	if _, err := m.AgentProfile("broken/reviewer"); err == nil {
		t.Fatal("partially activated invalid plugin")
	}
	menu, ok := m.pluginMenu(t.Context(), &command.Command{Args: []string{"enable"}})
	if !ok || len(menu.Items) != 1 || !strings.Contains(menu.Items[0].Description, "failed") {
		t.Fatalf("retry menu=%+v", menu)
	}
	if err := os.WriteFile(hookPath, []byte(`{"PreModel":[{"hooks":[{"type":"js","code":"({hint:{content:'loaded'}})"}]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPluginEnabled("broken", true); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot().HookErrors) != 0 {
		t.Fatal("stale hook error")
	}
	if got := policy.Resolve(m.policy.BeforeModel()); len(got.Hints) != 1 || got.Hints[0].Content != "loaded" {
		t.Fatalf("hook not activated: %+v", got)
	}
	if _, err := m.AgentProfile("broken/reviewer"); err != nil {
		t.Fatal(err)
	}
}
