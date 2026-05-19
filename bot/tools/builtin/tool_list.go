package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"
	"nekocode/bot/tools"

	"nekocode/common"
)

type ListTool struct{}

func (t *ListTool) Name() string                                       { return "list" }
func (t *ListTool) ExecutionMode(map[string]interface{}) tools.ExecutionMode { return tools.ModeParallel }
func (t *ListTool) DangerLevel(map[string]interface{}) common.DangerLevel     { return common.LevelSafe }
func (t *ListTool) Description() string {
	return "List directory contents. ALWAYS use List — NEVER invoke ls as Bash. Returns files and subdirectories sorted by name."
}

func (t *ListTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "Directory path to list"},
	}
}

func (t *ListTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("missing path parameter")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %v", err)
	}

	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			fmt.Fprintf(&sb, "▸ %s/\n", e.Name())
		} else {
			info, err := e.Info()
			if err != nil {
				fmt.Fprintf(&sb, "  %s\n", e.Name())
			} else {
				fmt.Fprintf(&sb, "  %s  %s\n", e.Name(), humanSize(info.Size()))
			}
		}
	}
	result := sb.String()
	if result == "" {
		result = "(empty)"
	}
	return result, nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
