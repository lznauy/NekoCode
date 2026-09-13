package catalog

import (
	"slices"
	"testing"

	edittool "nekocode/bot/extension/tool/builtin/filesystem/edit"
	listtool "nekocode/bot/extension/tool/builtin/filesystem/list"
	readtool "nekocode/bot/extension/tool/builtin/filesystem/read"
	searchtool "nekocode/bot/extension/tool/builtin/filesystem/search"
	treetool "nekocode/bot/extension/tool/builtin/filesystem/tree"
	writetool "nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/builtin/lsp"
	"nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/builtin/todo"
	"nekocode/bot/extension/tool/builtin/web"
	"nekocode/bot/extension/tool/runtime/core"
)

// TestInterface verifies Name / Params / ExecutionMode for every tool.
func TestInterface(t *testing.T) {
	lspTools := lsp.NewLSPTool().Tools()
	lspCases := make([]struct {
		tool      core.Tool
		name      string
		mode      core.ExecutionMode
		minParams int
	}, 0, 4)
	for _, tt := range lspTools {
		lspCases = append(lspCases, struct {
			tool      core.Tool
			name      string
			mode      core.ExecutionMode
			minParams int
		}{tt, tt.Name(), core.ModeParallel, 1})
	}
	tests := []struct {
		tool      core.Tool
		name      string
		mode      core.ExecutionMode
		minParams int
	}{
		{&readtool.ReadTool{}, "read", core.ModeParallel, 3},
		{&writetool.WriteTool{}, "write", core.ModeSequential, 2},
		{&edittool.EditTool{}, "edit", core.ModeSequential, 4},
		{&shell.ShellTool{}, "shell", core.ModeSequential, 5},
		{shell.NewProcessTool(&shell.ShellTool{}), "process", core.ModeSequential, 3},
		{&searchtool.GlobTool{}, "glob", core.ModeParallel, 1},
		{&searchtool.GrepTool{}, "grep", core.ModeParallel, 1},
		{&listtool.ListTool{}, "list", core.ModeParallel, 1},
		{&treetool.TreeTool{}, "tree", core.ModeParallel, 1},
		{&todo.TodoWriteTool{}, "todo_write", core.ModeSequential, 1},
		{task.NewTaskTool(), "task", core.ModeParallel, 1},
		{web.NewWebSearchTool(), "web_search", core.ModeParallel, 1},
		{web.NewWebFetchTool(), "web_fetch", core.ModeParallel, 1},
		{web.NewWebExtractTool(), "web_extract", core.ModeParallel, 1},
	}
	tests = append(tests, lspCases...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.tool.Name() != tt.name {
				t.Errorf("Name() = %q, want %q", tt.tool.Name(), tt.name)
			}
			if tt.tool.ExecutionMode(nil) != tt.mode {
				t.Errorf("ExecutionMode = %v, want %v", tt.tool.ExecutionMode(nil), tt.mode)
			}
			if n := len(tt.tool.Parameters()); n < tt.minParams {
				t.Errorf("Parameters() = %d, want >= %d", n, tt.minParams)
			}
			if tt.tool.Description() == "" {
				t.Error("Description() is empty")
			}
		})
	}
}

func TestEditParameters(t *testing.T) {
	params := (&edittool.EditTool{}).Parameters()
	got := make([]string, len(params))
	for i, param := range params {
		got[i] = param.Name
	}
	want := []string{"path", "oldString", "newString", "replaceAll"}
	if !slices.Equal(got, want) {
		t.Fatalf("edit parameters = %v, want %v", got, want)
	}
}
