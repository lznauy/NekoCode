package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/tools"
)

type WriteTool struct {
	WriteModeTool
}

func (t *WriteTool) Name() string { return "write" }
func (t *WriteTool) Description() string {
	return "Create or overwrite a file. Auto-creates parent dirs. Must Read existing files first (enforced). Use Edit for partial changes. Content: use \\n \\\" \\\\ for newlines/quotes/backslashes."
}

func (t *WriteTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path"},
		{Name: "content", Type: "string", Required: true, Description: "Content to write"},
	}
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := requireStringArg(args, "path")
	if err != nil {
		return "", err
	}
	content, err := requireStringArg(args, "content")
	if err != nil {
		return "", err
	}

	safePath, err := tools.ValidatePath(path)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}
	mode := getFileMode(safePath)
	if err := os.WriteFile(safePath, []byte(content), mode); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	// Record snapshot for hashline edit recovery within the session.
	tag := tools.RecordSnapshot(safePath, content)
	if tag != "" {
		return fmt.Sprintf("[%s#%s] Written (%d chars)", safePath, tag, len(content)), nil
	}
	return fmt.Sprintf("Written: %s (%d chars)", safePath, len(content)), nil
}
