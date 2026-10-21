// EditTool — content-anchored file editing.

package edit

import (
	"context"
	_ "embed"
	"fmt"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

// ---------------------------------------------------------------------------
// tool definition
// ---------------------------------------------------------------------------

type EditTool struct {
	toolutil.WriteModeTool
}

func (t *EditTool) Name() string { return "edit" }

//go:embed edit_description.md
var editDescription string

func (t *EditTool) Description() string {
	return editDescription
}

func (t *EditTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true,
			Description: "Absolute path to the file to edit."},
		{Name: "oldString", Type: "string", Required: false,
			Description: "Exact text to replace."},
		{Name: "newString", Type: "string", Required: false,
			Description: "Replacement text. Use an empty string to delete oldString."},
		{Name: "replaceAll", Type: "boolean", Required: false,
			Description: "Replace every exact match of oldString. Defaults to false, which requires one unique match."},
	}
}

// ---------------------------------------------------------------------------
// preview
// ---------------------------------------------------------------------------

// Preview reads the file, applies the content-anchored edit to a copy, and returns a diff.
func (t *EditTool) Preview(args map[string]any) string {
	return t.PreviewContext(context.Background(), args)
}

func (t *EditTool) PreviewContext(ctx context.Context, args map[string]any) string {
	path, _ := args["path"].(string)
	if path == "" {
		return ""
	}
	return t.previewEdit(ctx, args)
}

// ---------------------------------------------------------------------------
// execute
// ---------------------------------------------------------------------------

func (t *EditTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return core.ToolOutput{}, fmt.Errorf("path parameter is required")
	}

	toolText, toolErr := t.executeEdit(ctx, args)
	return core.ToolOutput{Text: toolText}, toolErr
}
