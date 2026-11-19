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
	return "Read text file contents; images return metadata only, PDFs return metadata and extraction guidance. " + t.imageGuidance() + " Read known workspace-relative or absolute paths directly; batch independent read calls before edit/write to obtain current-version receipts. Use glob to locate it if the path is unknown. Text: up to 500 lines per call, 64 MiB per file. Text output includes [path#TAG] followed by lineNo:content lines. If output is truncated, read a smaller range before editing."
}

func (t *ReadTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path, relative to the workspace root or absolute."},
		{Name: "startLine", Type: "integer", Description: "First line to read (1-based, default 1)"},
		{Name: "endLine", Type: "integer", Description: "Last line to read (inclusive, >= startLine). At most 500 lines per call, even with an explicit endLine."},
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
