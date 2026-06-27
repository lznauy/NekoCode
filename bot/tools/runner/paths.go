package runner

import (
	"nekocode/bot/tools/core"
	"nekocode/bot/tools/pathutil"
)

func confirmArgs(_ string, args map[string]any) map[string]any {
	return args
}

func toolPaths(tc core.ToolCallItem) []string {
	if p, ok := tc.Args["path"].(string); ok && p != "" {
		return []string{p}
	}
	return nil
}

func validatePath(path string) (string, error) {
	return pathutil.ValidatePath(path)
}
