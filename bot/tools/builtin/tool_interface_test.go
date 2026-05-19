package builtin

import (
	"testing"

	"nekocode/bot/tools"
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
		{&ReadTool{}, "read", tools.ModeParallel, common.LevelSafe, 3},
		{&WriteTool{}, "write", tools.ModeSequential, common.LevelWrite, 2},
		{&EditTool{}, "edit", tools.ModeSequential, common.LevelWrite, 3},
		{&BashTool{}, "bash", tools.ModeSequential, common.LevelWrite, 1},
		{&GlobTool{}, "glob", tools.ModeParallel, common.LevelSafe, 1},
		{&GrepTool{}, "grep", tools.ModeParallel, common.LevelSafe, 1},
		{&ListTool{}, "list", tools.ModeParallel, common.LevelSafe, 1},
		{&TreeTool{}, "tree", tools.ModeParallel, common.LevelSafe, 1},
		{&TodoWriteTool{}, "todo_write", tools.ModeSequential, common.LevelSafe, 1},
		{&TaskTool{}, "task", tools.ModeParallel, common.LevelSafe, 1},
		{&WebSearchTool{}, "web_search", tools.ModeParallel, common.LevelSafe, 1},
		{&WebFetchTool{}, "web_fetch", tools.ModeParallel, common.LevelSafe, 1},
		{&ProjectInfoTool{}, "project_info", tools.ModeParallel, common.LevelSafe, 1},
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
