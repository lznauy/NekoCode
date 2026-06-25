// EditTool — edit-aware JSON intent file editing.

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
		{Name: "patch", Type: "string", Required: true,
			Description: "JSON edit intent. When revert=true, use bare file path instead."},
		{Name: "revert", Type: "boolean", Required: false,
			Description: "Set to true to revert file to its pre-edit state. Patch should be the bare file path."},
	}
}

// ---------------------------------------------------------------------------
// preview
// ---------------------------------------------------------------------------

// Preview reads files, applies the JSON intent to a copy, and returns a diff.
func (t *EditTool) Preview(args map[string]any) string {
	patchStr, _ := args["patch"].(string)
	if patchStr == "" {
		return ""
	}
	if rv, _ := args["revert"].(bool); rv {
		return fmt.Sprintf("(revert: %s)", filepath.Base(patchStr))
	}
	if !isJSONIntent(patchStr) {
		return "(edit patch must be a JSON intent object; re-read the target range and retry)"
	}
	return t.previewIntent(patchStr)
}

// ---------------------------------------------------------------------------
// execute
// ---------------------------------------------------------------------------

func (t *EditTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	patchStr, ok := args["patch"].(string)
	if !ok || patchStr == "" {
		return "", fmt.Errorf("patch parameter is required")
	}

	// Revert mode: restore file from pre-edit snapshot.
	if rv, _ := args["revert"].(bool); rv {
		return t.revertSnapshot(patchStr)
	}
	if !isJSONIntent(patchStr) {
		return "", fmt.Errorf("edit patch must be a JSON intent object")
	}
	return t.executeIntent(ctx, patchStr)
}
