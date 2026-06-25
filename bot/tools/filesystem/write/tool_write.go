package write

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/tools"
	"nekocode/bot/tools/toolhelpers"
)

type WriteTool struct {
	toolhelpers.WriteModeTool
}

func (t *WriteTool) Name() string { return "write" }
func (t *WriteTool) Description() string {
	return "Create or overwrite a file. Auto-creates parent dirs. " +
		"For existing files, Read first to confirm current content — the governance layer tracks reads and warns if a file is written without prior Read. " +
		"For partial changes, prefer Edit JSON intents over Write — it produces minimal diffs and auto-snapshots for undo. " +
		"Content escaping: use \\n for newlines, \\\" for quotes, \\\\ for backslashes."
}

func (t *WriteTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path"},
		{Name: "content", Type: "string", Required: true, Description: "Content to write"},
	}
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := toolhelpers.RequireStringArg(args, "path")
	if err != nil {
		return "", err
	}
	content, err := toolhelpers.RequireStringArg(args, "content")
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
	mode := toolhelpers.GetFileMode(safePath)
	if err := os.WriteFile(safePath, []byte(content), mode); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	// Record snapshot for read/edit continuity within the session.
	tag := tools.RecordSnapshot(safePath, content)
	if tag != "" {
		return fmt.Sprintf("[%s#%s] Written (%d chars)", safePath, tag, len(content)), nil
	}
	return fmt.Sprintf("Written: %s (%d chars)", safePath, len(content)), nil
}
