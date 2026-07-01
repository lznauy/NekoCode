package toolhelpers

import (
	"fmt"
	"os"

	"nekocode/bot/tools/core"
	"nekocode/common"
)

type SafeReadOnlyTool struct{}

func (t *SafeReadOnlyTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeParallel
}
func (t *SafeReadOnlyTool) DangerLevel(map[string]any) common.DangerLevel { return common.LevelSafe }

type SequentialSafeTool struct{}

func (t *SequentialSafeTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}
func (t *SequentialSafeTool) DangerLevel(map[string]any) common.DangerLevel { return common.LevelSafe }

type WriteModeTool struct{}

func (t *WriteModeTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}
func (t *WriteModeTool) DangerLevel(map[string]any) common.DangerLevel { return common.LevelWrite }

func RequireStringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("missing %s parameter", key)
	}
	return v, nil
}

func RequireIntArg(args map[string]any, key string) (int, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing %s parameter", key)
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid %s: expected number, got %T", key, v)
	}
	return int(f), nil
}

func OptStringArg(args map[string]any, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

func OptIntArg(args map[string]any, key string, def int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return def
}

func ClampIntArg(args map[string]any, key string, def, min, max int) int {
	v, ok := args[key].(float64)
	if !ok {
		return def
	}
	n := int(v)
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func GetFileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode()
	}
	return 0644
}
