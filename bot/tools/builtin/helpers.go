// helpers.go — 共享的参数提取/验证辅助函数，消除 builtin 工具中的重复样板代码。
package builtin

import (
	"fmt"
	"os"

	"nekocode/bot/tools"
	"nekocode/common"
)

// SafeReadOnlyTool is a base struct for read-only tools.
// Embed it to get default ExecutionMode (Parallel) and DangerLevel (Safe).
type SafeReadOnlyTool struct{}

func (t *SafeReadOnlyTool) ExecutionMode(map[string]any) tools.ExecutionMode { return tools.ModeParallel }
func (t *SafeReadOnlyTool) DangerLevel(map[string]any) common.DangerLevel    { return common.LevelSafe }

// SequentialSafeTool is a base struct for sequential, safe tools.
type SequentialSafeTool struct{}

func (t *SequentialSafeTool) ExecutionMode(map[string]any) tools.ExecutionMode { return tools.ModeSequential }
func (t *SequentialSafeTool) DangerLevel(map[string]any) common.DangerLevel    { return common.LevelSafe }

// WriteModeTool is a base struct for sequential, write-level tools.
type WriteModeTool struct{}

func (t *WriteModeTool) ExecutionMode(map[string]any) tools.ExecutionMode { return tools.ModeSequential }
func (t *WriteModeTool) DangerLevel(map[string]any) common.DangerLevel    { return common.LevelWrite }

// requireStringArg 提取必需的字符串参数，缺失或为空时返回错误。
func requireStringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("missing %s parameter", key)
	}
	return v, nil
}

// requireIntArg 提取必需的整数参数，缺失或类型不匹配时返回错误。
func requireIntArg(args map[string]any, key string) (int, error) {
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

// optStringArg 提取可选字符串参数，缺失时返回默认值。
func optStringArg(args map[string]any, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

// optIntArg 提取可选整数参数，缺失时返回默认值。
func optIntArg(args map[string]any, key string, def int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return def
}

// clampIntArg 提取可选整数参数并钳制到 [min, max] 范围，缺失时返回默认值。
func clampIntArg(args map[string]any, key string, def, min, max int) int {
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

// getFileMode 获取文件权限模式，文件不存在时返回默认 0644。
func getFileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode()
	}
	return 0644
}
