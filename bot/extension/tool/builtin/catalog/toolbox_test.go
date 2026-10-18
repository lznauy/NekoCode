package catalog

import (
	"path/filepath"
	"testing"

	"nekocode/bot/config"
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
