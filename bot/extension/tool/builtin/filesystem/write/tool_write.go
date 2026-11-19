package write

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/extension/tool/builtin/diff"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/protocol"
)

// This limits echoes of model-authored content, independently of the shared
// output budget that applies to every tool result.
const writeEchoLimitBytes = 2048

type WriteTool struct {
	toolutil.WriteModeTool
}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Description() string {
	return "Create or overwrite a file. Auto-creates parent dirs. " +
		"For existing files, read the current version first. A delivered successful write/edit receipt can be reused while that exact version is unchanged. Read again after external modification, receipt removal or context compaction; if output is truncated, read smaller ranges. " +
		"For partial changes, prefer Edit with oldString/newString over Write — it produces minimal diffs; use /rewind to undo completed file changes. " +
		"Content escaping: use \\n for newlines, \\\" for quotes, \\\\ for backslashes."
}

func (t *WriteTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path"},
		{Name: "content", Type: "string", Description: "Complete file content, including empty string to clear. Supply content or reuse."},
		{Name: "reuse", Type: "string", Description: "Draft ID from a blocked write in this run. After reading the same path, use instead of content if the proposed replacement still fits."},
	}
}

// Preview shows a diff of what will be written vs existing content (if file exists).
func (t *WriteTool) Preview(args map[string]any) string {
	return t.PreviewContext(context.Background(), args)
}

func (t *WriteTool) PreviewContext(ctx context.Context, args map[string]any) string {
	path, _ := args["path"].(string)
	content, err := toolutil.StringArg(args, "content")
	if err != nil {
		return ""
	}
	if path == "" {
		return ""
	}

	safePath, err := toolutil.ValidatePathWritableContext(ctx, path)
	if err != nil {
		return ""
	}

	return formatWriteDiff(path, existingContent(safePath), content)
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	if _, unresolved := args["reuse"]; unresolved {
		return core.ToolOutput{}, fmt.Errorf("unresolved write draft; supply full content")
	}
	path, err := toolutil.RequireStringArg(args, "path")
	if err != nil {
		return core.ToolOutput{}, err
	}
	content, err := toolutil.StringArg(args, "content")
	if err != nil {
		return core.ToolOutput{}, err
	}

	safePath, err := toolutil.ValidatePathWritableContext(ctx, path)
	if err != nil {
		return core.ToolOutput{}, err
	}

	unlock, err := execution.LockFile(ctx, safePath)
	if err != nil {
		return core.ToolOutput{}, err
	}
	defer unlock()
	checked, err := toolutil.ValidatePathWritableContext(ctx, path)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if checked != safePath {
		return core.ToolOutput{}, fmt.Errorf("write target changed while waiting; retry after reading it")
	}
	data, readErr := os.ReadFile(safePath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return core.ToolOutput{}, fmt.Errorf("cannot inspect write target: %w", readErr)
	}
	if args["_existing_draft"] == true && os.IsNotExist(readErr) {
		return core.ToolOutput{}, fmt.Errorf("draft target no longer exists; review the path before creating a file")
	}
	reads := execution.FileReadsFromContext(ctx)
	if readErr == nil {
		if err := reads.Check(safePath, data); err != nil {
			return core.ToolOutput{}, err
		}
	}
	existing := string(data)

	if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
		return core.ToolOutput{}, fmt.Errorf("failed to create directory: %w", err)
	}
	mode := toolutil.GetFileMode(safePath)
	if err := os.WriteFile(safePath, []byte(content), mode); err != nil {
		reads.Invalidate(safePath)
		return core.ToolOutput{}, fmt.Errorf("failed to write file: %w", err)
	}
	reads.Invalidate(safePath)
	if cache := execution.FileCacheFromContext(ctx); cache != nil {
		cache.Invalidate(safePath)
	}
	// Record snapshot for read/edit continuity within the session.
	tag := execution.RecordSnapshotInContext(ctx, safePath, content)

	diffOutput := formatWriteDiff(safePath, existing, content)
	confirmation := fmt.Sprintf("Written: %s (%d bytes).", safePath, len(content))
	if tag != "" {
		confirmation = fmt.Sprintf("[%s#%s] Written (%d bytes).", safePath, tag, len(content))
	}
	output := core.ToolOutput{Text: confirmation}
	if diffOutput != "" {
		output.Text = diffOutput
		if tag != "" {
			output.Text = fmt.Sprintf("[%s#%s]\n%s", safePath, tag, diffOutput)
		}
		// Preserve the full evidence before replacing an echo of model-authored
		// content. On preservation failure retain the original inline diff.
		if state := execution.ExecutionStateFromContext(ctx); state != nil && state.OutputStore != nil && len(output.Text) > writeEchoLimitBytes {
			if artifact, err := state.OutputStore.PreserveOutput(output.Text); err == nil {
				output = core.ToolOutput{Text: confirmation, Artifact: protocol.ToolOutputArtifact{Path: artifact, FullDiff: true}}
			}
		}
	}
	return execution.MutationOutput(ctx, safePath, []byte(content), output), nil
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
