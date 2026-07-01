package write

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/tools"
	"nekocode/bot/tools/core"
	"nekocode/bot/tools/diff"
	"nekocode/bot/tools/toolhelpers"
)

type WriteTool struct {
	toolhelpers.WriteModeTool
}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Description() string {
	return "Create or overwrite a file. Auto-creates parent dirs. " +
		"For existing files, Read first to confirm current content — the policy layer tracks reads and warns if a file is written without prior Read. " +
		"For partial changes, prefer Edit with oldString/newString over Write — it produces minimal diffs and auto-snapshots for undo. " +
		"Content escaping: use \\n for newlines, \\\" for quotes, \\\\ for backslashes."
}

func (t *WriteTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path"},
		{Name: "content", Type: "string", Required: true, Description: "Content to write"},
	}
}

// Preview shows a diff of what will be written vs existing content (if file exists).
func (t *WriteTool) Preview(args map[string]any) string {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return ""
	}

	safePath, err := tools.ValidatePath(path)
	if err != nil {
		return ""
	}

	return formatWriteDiff(path, existingContent(safePath), content)
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

	existing := existingContent(safePath)

	if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}
	mode := toolhelpers.GetFileMode(safePath)
	if err := os.WriteFile(safePath, []byte(content), mode); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	// Record snapshot for read/edit continuity within the session.
	tag := tools.RecordSnapshot(safePath, content)

	diffOutput := formatWriteDiff(safePath, existing, content)
	if diffOutput == "" {
		if tag != "" {
			return fmt.Sprintf("[%s#%s] Written (%d chars)", safePath, tag, len(content)), nil
		}
		return fmt.Sprintf("Written: %s (%d chars)", safePath, len(content)), nil
	}

	if tag != "" {
		return fmt.Sprintf("[%s#%s]\n%s", safePath, tag, diffOutput), nil
	}
	return diffOutput, nil
}

func formatWriteDiff(path, existing, content string) string {
	return diff.RenderTextChange(existing, content, diff.TextChangeOptions{
		Context: diff.DefaultContext,
		Header:  diff.ToolHeader("write", path),
	})
}

func existingContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
