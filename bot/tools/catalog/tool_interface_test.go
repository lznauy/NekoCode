package catalog

import (
	"testing"

	"nekocode/bot/tools"
	edittool "nekocode/bot/tools/filesystem/edit"
	listtool "nekocode/bot/tools/filesystem/list"
	readtool "nekocode/bot/tools/filesystem/read"
	searchtool "nekocode/bot/tools/filesystem/search"
	treetool "nekocode/bot/tools/filesystem/tree"
	writetool "nekocode/bot/tools/filesystem/write"
	"nekocode/bot/tools/shell"
	"nekocode/bot/tools/tasktool"
	"nekocode/bot/tools/todo"
	"nekocode/bot/tools/web"
	"nekocode/common"
)

// TestInterface verifies Name / Params / ExecutionMode / DangerLevel for every tool.
func TestInterface(t *testing.T) {
	tests := []struct {
		tool      tools.Tool
		name      string
		mode      tools.ExecutionMode
		level     common.DangerLevel
		minParams int
	}{
		{&readtool.ReadTool{}, "read", tools.ModeParallel, common.LevelSafe, 3},
		{&writetool.WriteTool{}, "write", tools.ModeSequential, common.LevelWrite, 2},
		{&edittool.EditTool{}, "edit", tools.ModeSequential, common.LevelWrite, 1},
		{&shell.BashTool{}, "bash", tools.ModeSequential, common.LevelWrite, 1},
		{&searchtool.GlobTool{}, "glob", tools.ModeParallel, common.LevelSafe, 1},
		{&searchtool.GrepTool{}, "grep", tools.ModeParallel, common.LevelSafe, 1},
		{&listtool.ListTool{}, "list", tools.ModeParallel, common.LevelSafe, 1},
		{&treetool.TreeTool{}, "tree", tools.ModeParallel, common.LevelSafe, 1},
		{&todo.TodoWriteTool{}, "todo_write", tools.ModeSequential, common.LevelSafe, 1},
		{tasktool.NewTaskTool(), "task", tools.ModeParallel, common.LevelSafe, 1},
		{web.NewWebSearchTool(), "web_search", tools.ModeParallel, common.LevelSafe, 1},
		{web.NewWebFetchTool(), "web_fetch", tools.ModeParallel, common.LevelSafe, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.tool.Name() != tt.name {
				t.Errorf("Name() = %q, want %q", tt.tool.Name(), tt.name)
			}
			if tt.tool.ExecutionMode(nil) != tt.mode {
				t.Errorf("ExecutionMode = %v, want %v", tt.tool.ExecutionMode(nil), tt.mode)
			}
			if tt.tool.DangerLevel(nil) != tt.level {
				t.Errorf("DangerLevel = %v, want %v", tt.tool.DangerLevel(nil), tt.level)
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
