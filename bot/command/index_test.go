package command

import (
	tools "nekocode/bot/extension/tool"
	index "nekocode/bot/extension/tool/builtin/index"
	"nekocode/bot/extension/tool/runtime/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexCommandMenu(t *testing.T) {
	h := New(Deps{})
	menu, ok := h.Menu(t.Context(), "/index")
	if !ok || len(menu.Items) != 2 {
		t.Fatalf("index menu = %+v, %v", menu, ok)
	}
	if menu.Items[0].Value != "/index status" || !menu.Items[0].Submit ||
		menu.Items[1].Value != "/index rebuild" || !menu.Items[1].Submit {
		t.Fatalf("index menu items = %+v", menu.Items)
	}
	if _, ok := h.Menu(t.Context(), "/index status"); ok {
		t.Fatal("direct index action unexpectedly opened a menu")
	}
}

func TestIndexCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tool := index.NewIndexTool()
	defer tool.Close()
	registry := tools.New(tool)
	registry.SetWorkspace(workspace.New(root, nil))
	h := New(Deps{ToolRegistry: registry})
	for input, want := range map[string]string{"/index": "Files: 0", "/index status": "Watcher: running", "/index rebuild": "Index rebuilt.", "/index invalid": "Usage:", "/index status extra": "Usage:"} {
		got, handled := h.Execute(t.Context(), input, nil)
		if !handled || !strings.Contains(got, want) {
			t.Fatalf("%s: %s (%v)", input, got, handled)
		}
	}
	if command, duringTask := h.CommandAvailability("/index rebuild"); !command || duringTask {
		t.Fatal("rebuild must not run during a task")
	}
	h = New(Deps{})
	got, _ := h.Execute(t.Context(), "/index", nil)
	if !strings.Contains(got, "unavailable") {
		t.Fatal(got)
	}
}
