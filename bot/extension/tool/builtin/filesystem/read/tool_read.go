package read

import (
	"context"
	"path/filepath"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

type ReadTool struct {
	toolutil.SafeReadOnlyTool
}

func (t *ReadTool) Name() string { return "read" }
func (t *ReadTool) Description() string {
	return "Read file contents (text, images, PDF). Absolute path required. Use startLine/endLine for range, max 500 lines. Text output includes [path#TAG], a VIEW line with rev/window metadata for JSON edit intents, then clean lineNo:content lines."
}

func (t *ReadTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path (absolute)"},
		{Name: "startLine", Type: "integer", Required: true, Description: "First line to read (1-based)"},
		{Name: "endLine", Type: "integer", Required: true, Description: "Last line to read (inclusive, >= startLine)"},
	}
}

const maxReadLines = 500

func (t *ReadTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	path, err := toolutil.RequireStringArg(args, "path")
	if err != nil {
		return core.ToolOutput{}, err
	}
	safePath, err := toolutil.ValidatePathReadableContext(ctx, path)
	if err != nil {
		return core.ToolOutput{}, err
	}
	switch strings.ToLower(filepath.Ext(safePath)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		toolText, toolErr := t.readImage(safePath)
		return core.ToolOutput{Text: toolText}, toolErr
	case ".pdf":
		toolText, toolErr := t.readPDF(safePath)
		return core.ToolOutput{Text: toolText}, toolErr
	default:
		toolText, toolErr := t.readTextCached(ctx, safePath, args)
		return core.ToolOutput{Text: toolText}, toolErr
	}
}
