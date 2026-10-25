package catalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/config"
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
