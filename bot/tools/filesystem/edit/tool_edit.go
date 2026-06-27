// EditTool — content-anchored file editing.

package edit

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"

	"nekocode/bot/tools"
	"nekocode/bot/tools/toolhelpers"
)

// ---------------------------------------------------------------------------
// tool definition
// ---------------------------------------------------------------------------

type EditTool struct {
	toolhelpers.WriteModeTool
}

func (t *EditTool) Name() string { return "edit" }

//go:embed edit_description.md
var editDescription string

func (t *EditTool) Description() string {
	return editDescription
}

func (t *EditTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true,
			Description: "Absolute path to the file to edit."},
		{Name: "oldString", Type: "string", Required: false,
			Description: "Exact text to replace. Required unless revert=true."},
		{Name: "newString", Type: "string", Required: false,
			Description: "Replacement text. Use an empty string to delete oldString."},
		{Name: "replaceAll", Type: "boolean", Required: false,
			Description: "Replace every exact match of oldString. Defaults to false, which requires one unique match."},
		{Name: "revert", Type: "boolean", Required: false,
			Description: "Set to true to restore the file to its pre-edit snapshot."},
	}
}

// ---------------------------------------------------------------------------
// preview
// ---------------------------------------------------------------------------

// Preview reads the file, applies the content-anchored edit to a copy, and returns a diff.
func (t *EditTool) Preview(args map[string]any) string {
	path, _ := args["path"].(string)
	if path == "" {
		return ""
	}
	if rv, _ := args["revert"].(bool); rv {
		return fmt.Sprintf("(revert: %s)", filepath.Base(path))
	}
	return t.previewEdit(args)
}

// ---------------------------------------------------------------------------
// execute
// ---------------------------------------------------------------------------

func (t *EditTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path parameter is required")
	}

	// Revert mode: restore file from pre-edit snapshot.
	if rv, _ := args["revert"].(bool); rv {
		return t.revertSnapshot(path)
	}
	return t.executeEdit(ctx, args)
}
