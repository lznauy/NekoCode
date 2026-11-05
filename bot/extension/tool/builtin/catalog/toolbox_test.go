package catalog

import (
	"context"
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/runner"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestToolboxConditionallyRegistersImageUnderstand(t *testing.T) {
	without := NewToolboxWithConfig(ToolboxConfig{})
	t.Cleanup(func() { _ = without.Close() })
	if _, err := without.Registry.Lookup("image_understand"); err == nil {
		t.Fatal("image_understand should not be registered without configuration")
	}

	with := NewToolboxWithConfig(ToolboxConfig{ImageUnderstandModels: []config.ImageUnderstandConfig{{
		Name: "vision", Provider: "openai", Model: "vision-1",
	}}})
	t.Cleanup(func() { _ = with.Close() })
	if _, err := with.Registry.Lookup("image_understand"); err != nil {
		t.Fatalf("image_understand should be registered with configuration: %v", err)
	}
}

func TestToolboxScopesTemporaryRootsBySession(t *testing.T) {
	box := NewToolboxWithConfig(ToolboxConfig{})
	t.Cleanup(func() { _ = box.Close() })
	primary := t.TempDir()
	extra := t.TempDir()
	box.Workspace().Configure(primary, nil)
	box.SetSessionID("one")
	if _, err := box.Workspace().AddSessionRoot(extra, workspace.AccessReadOnly); err != nil {
		t.Fatal(err)
	}

	box.SetSessionID("two")
	if _, _, ok, err := box.Workspace().CheckRead(filepath.Join(extra, "note.md")); err != nil || ok {
		t.Fatalf("temporary root leaked into another session: ok=%v err=%v", ok, err)
	}
	if err := box.CloseSession("one"); err != nil {
		t.Fatal(err)
	}
	box.SetSessionID("one")
	if _, _, ok, err := box.Workspace().CheckRead(filepath.Join(extra, "note.md")); err != nil || ok {
		t.Fatalf("closed session retained temporary root: ok=%v err=%v", ok, err)
	}
}

func TestTaskListSurvivesRegistryRebuildAndAllowsPlanMode(t *testing.T) {
	box := NewToolboxWithConfig(ToolboxConfig{})
	defer box.Close()
	calls := 0
	box.WireTaskLister(func(context.Context, taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		calls++
		return taskbridge.TaskPage{SessionID: "parent"}, nil
	})
	for i := 0; i < 2; i++ {
		entry, err := box.Registry.Lookup("task_list")
		if err != nil || !entry.PlanAllowed {
			t.Fatalf("task_list registration = %+v, %v", entry, err)
		}
		out, err := entry.Tool.Execute(context.Background(), nil)
		if err != nil || !strings.Contains(out.Text, "parent") {
			t.Fatalf("task_list output = %q, %v", out.Text, err)
		}
		box.RebuildRegistryWithConfig(ToolboxConfig{})
	}
	if calls != 2 {
		t.Fatalf("lister calls = %d", calls)
	}
	entry, err := box.Registry.Lookup("task")
	if err != nil || entry.PlanAllowed {
		t.Fatalf("task unexpectedly allowed in plan mode: %+v, %v", entry, err)
	}
}

func TestToolboxPreservesOwnedIndexOnRegistryRebuild(t *testing.T) {
	manager := workspace.New(t.TempDir(), nil)
	box := NewToolboxWithConfig(ToolboxConfig{Workspace: manager})
	defer box.Close()
	index := box.index
	box.RebuildRegistryWithConfig(ToolboxConfig{})
	if box.index != index || box.Workspace() != manager {
		t.Fatal("registry rebuild replaced owned resources")
	}
	if err := box.Close(); err != nil {
		t.Fatal(err)
	}
	if box.index != nil {
		t.Fatal("toolbox retained closed index")
	}
}

func TestReferenceToolSurvivesRebuildWithPlanReadOnlyRegistration(t *testing.T) {
	box := NewToolboxWithConfig(ToolboxConfig{})
	defer box.Close()
	calls := 0
	box.WireReferences(func(context.Context, refbridge.Query) (refbridge.Page, error) {
		calls++
		return refbridge.Page{SessionID: "parent"}, nil
	})
	for range 2 {
		entry, err := box.Registry.Lookup("context_refs")
		if err != nil || !entry.PlanAllowed {
			t.Fatalf("registration: %+v %v", entry, err)
		}
		out, err := entry.Tool.Execute(t.Context(), map[string]any{"action": "list"})
		if err != nil || !strings.Contains(out.Text, "parent") {
			t.Fatalf("reference output: %q %v", out.Text, err)
		}
		box.RebuildRegistryWithConfig(ToolboxConfig{})
	}
	if calls != 2 {
		t.Fatal("reader lost during rebuild")
	}
}

func TestNavigationRemainsLazyAndIndexExecutesInPlanMode(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fixture\n", "main.go": "package fixture\nfunc NavigationTarget() {}\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	box := NewToolboxWithConfig(ToolboxConfig{Workspace: workspace.New(root, nil)})
	defer box.Close()
	for range 2 {
		for _, profile := range agentprofile.Builtins() {
			for _, name := range profile.Tools {
				if _, err := box.Registry.Lookup(name); err != nil {
					t.Fatalf("%s has unregistered tool %s", profile.Name, name)
				}
			}
		}
		for _, name := range []string{"index", "tree", "diff", "lsp_definition", "lsp_references", "lsp_hover", "lsp_diagnostics"} {
			entry, err := box.Registry.Lookup(name)
			if err != nil || !entry.PlanAllowed {
				t.Fatalf("%s not usable during planning: %+v %v", name, entry, err)
			}
		}
		box.RebuildRegistryWithConfig(ToolboxConfig{})
	}
	if _, err := os.Stat(filepath.Join(root, ".nekocode")); !os.IsNotExist(err) {
		t.Fatalf("tool registration initialized index: %v", err)
	}
	executor := runner.NewExecutor(box.Registry)
	executor.SetPlanMode(true)
	result := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "navigate", Name: "index", Args: map[string]any{"query": "symbol:NavigationTarget"}}})[0]
	if result.Error != "" || !strings.Contains(result.Output, "NavigationTarget") {
		t.Fatalf("plan index query failed: %+v", result)
	}
	failed := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "bad-search", Name: "index", Args: map[string]any{"query": `search:"`}}})[0]
	if failed.Error == "" || len(failed.Evidence.Executions) != 1 || failed.Evidence.Executions[0].Status != "error" {
		t.Fatalf("index failure produced successful execution evidence: %+v", failed)
	}
	fallback := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "read-fallback", Name: "read", Args: map[string]any{"path": filepath.Join(root, "main.go"), "startLine": float64(1), "endLine": float64(2)}}})[0]
	if fallback.Error != "" || !strings.Contains(fallback.Output, "NavigationTarget") {
		t.Fatalf("read fallback unavailable after index failure: %+v", fallback)
	}
	before, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []core.ToolCallItem{
		{ID: "inspect-tree", Name: "tree", Args: map[string]any{"path": root}},
		{ID: "compare", Name: "diff", Args: map[string]any{"old": "path:" + filepath.Join(root, "main.go"), "new": "package fixture\nfunc NavigationTarget() { /* preview */ }\n"}},
	} {
		out := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{call})[0]
		want := "main.go"
		if call.Name == "diff" {
			want = "preview"
		}
		if out.Error != "" || !strings.Contains(out.Output, want) {
			t.Fatalf("plan %s failed: %+v", call.Name, out)
		}
	}
	after, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("read-only comparison modified source: %v", err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("outside-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, call := range []core.ToolCallItem{
		{ID: "outside-tree", Name: "tree", Args: map[string]any{"path": outside}},
		{ID: "outside-diff", Name: "diff", Args: map[string]any{"old": "path:" + secret, "new": "plain text"}},
	} {
		out := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{call})[0]
		if out.Error == "" || strings.Contains(out.Output, "outside-secret") {
			t.Fatalf("%s escaped workspace: %+v", call.Name, out)
		}
	}
	blocked := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "write", Name: "write", Args: map[string]any{"path": filepath.Join(root, "forbidden.go"), "content": "package fixture"}}})[0]
	if blocked.Error != "plan mode: blocked" {
		t.Fatalf("planning gained source write capability: %+v", blocked)
	}
	if _, err := os.Stat(filepath.Join(root, "forbidden.go")); !os.IsNotExist(err) {
		t.Fatal("plan wrote a source file")
	}
}
