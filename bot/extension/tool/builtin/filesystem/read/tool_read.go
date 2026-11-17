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
	ImageUnderstandingUnavailable bool
}

func (t *ReadTool) Name() string { return "read" }
func (t *ReadTool) Description() string {
	return "Read text file contents; images return metadata only, PDFs return metadata and extraction guidance. " + t.imageGuidance() + " Absolute path required. Omit startLine/endLine to read the first 500 lines; with startLine alone, read up to 500 lines from there. Range parameters apply only to text. Text files are limited to 64 MiB. Text output includes [path#TAG] followed by lineNo:content lines. If output is truncated, read a smaller range before editing."
}

func (t *ReadTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path (absolute)"},
		{Name: "startLine", Type: "integer", Description: "First line to read (1-based, default 1)"},
		{Name: "endLine", Type: "integer", Description: "Last line to read (inclusive, >= startLine). Default up to 500 lines from startLine."},
	}
}

const maxReadLines = 500

func (t *ReadTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolOutput{}, err
	}
	path, err := toolutil.RequireStringArg(args, "path")
	if err != nil {
		return core.ToolOutput{}, err
	}
	safePath, err := toolutil.ValidatePathReadableContext(ctx, path)
	if err != nil {
		return core.ToolOutput{}, err
	}
	switch strings.ToLower(filepath.Ext(safePath)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		toolText, toolErr := t.readImage(ctx, safePath)
		return core.ToolOutput{Text: toolText}, toolErr
	case ".pdf":
		toolText, toolErr := t.readPDF(safePath)
		return core.ToolOutput{Text: toolText}, toolErr
	default:
		return t.readTextCached(ctx, safePath, args)
	}
}
