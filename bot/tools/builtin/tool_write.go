package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"nekocode/bot/tools"

	"nekocode/common"
)

type WriteTool struct{}

func (t *WriteTool) Name() string                                       { return "write" }
func (t *WriteTool) ExecutionMode(map[string]interface{}) tools.ExecutionMode { return tools.ModeSequential }
func (t *WriteTool) DangerLevel(map[string]interface{}) common.DangerLevel     { return common.LevelWrite }
func (t *WriteTool) Description() string {
	return "Create or overwrite a file. Auto-creates parent dirs. Must Read existing files first (enforced). Use Edit for partial changes. Content: use \\n \\\" \\\\ for newlines/quotes/backslashes."
}

func (t *WriteTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path"},
		{Name: "content", Type: "string", Required: true, Description: "Content to write"},
	}
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return "", fmt.Errorf("missing path parameter")
	}

	safePath, err := tools.ValidatePath(path)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(safePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %v", err)
	}
	return fmt.Sprintf("Written: %s (%d chars)", safePath, len(content)), nil
}
