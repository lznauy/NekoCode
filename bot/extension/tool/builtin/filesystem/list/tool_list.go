package list

import (
	"context"
	"fmt"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"os"
	"strings"
)

type ListTool struct {
	toolutil.SafeReadOnlyTool
}

func (t *ListTool) Name() string { return "list" }
func (t *ListTool) Description() string {
	return "List directory contents, including hidden dotfiles. ALWAYS use List — NEVER invoke ls or ls -al as Bash. Returns files and subdirectories sorted by name."
}

func (t *ListTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "Directory path to list; hidden dotfiles are included"},
	}
}

func (t *ListTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	path, err := toolutil.RequireStringArg(args, "path")
	if err != nil {
		return core.ToolOutput{}, err
	}

	safePath, err := toolutil.ValidatePathReadableContext(ctx, path)
	if err != nil {
		return core.ToolOutput{}, err
	}

	entries, err := os.ReadDir(safePath)
	if err != nil {
		return core.ToolOutput{}, fmt.Errorf("failed to read directory: %w", err)
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
	return core.ToolOutput{Text: result}, nil
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
