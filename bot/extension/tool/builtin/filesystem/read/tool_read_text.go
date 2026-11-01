package read

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

func (t *ReadTool) readTextCached(ctx context.Context, path string, args map[string]any) (core.ToolOutput, error) {
	startLine, err := toolutil.RequireIntArg(args, "startLine")
	if err != nil {
		return core.ToolOutput{}, err
	}
	endLine, err := toolutil.RequireIntArg(args, "endLine")
	if err != nil {
		return core.ToolOutput{}, err
	}
	unlock, err := execution.LockFile(ctx, path)
	if err != nil {
		return core.ToolOutput{}, err
	}
	defer unlock()
	requested, _ := args["path"].(string)
	checked, err := toolutil.ValidatePathReadableContext(ctx, requested)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if checked != path {
		return core.ToolOutput{}, fmt.Errorf("read target changed while waiting; retry the read")
	}
	data, err := readFileData(path)
	if err != nil {
		return core.ToolOutput{}, err
	}
	version := sha256.Sum256(data)
	cache := execution.FileCacheFromContext(ctx)
	var fullLines []string
	if cache != nil {
		fullLines, _ = cache.Lines(path, version)
	}
	if fullLines == nil {
		fullLines = strings.Split(toolutil.NormalizeText(string(data)), "\n")
	}
	startLine = max(1, startLine)
	if endLine < startLine || startLine > len(fullLines) {
		return core.ToolOutput{}, fmt.Errorf("invalid read range %d..%d (total %d)", startLine, endLine, len(fullLines))
	}
	result := formatReadOutput(ctx, path, fullLines, startLine, endLine)
	if cache != nil {
		end := min(startLine+min(endLine-startLine+1, maxReadLines)-1, len(fullLines))
		cache.Put(path, fullLines, startLine, end, version)
	}
	return core.ToolOutput{Text: result, ReadID: execution.FileReadsFromContext(ctx).Record(path, data, result)}, nil
}

func readFileData(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			msg := fmt.Sprintf("File not found: %s", filepath.Base(path))
			if s := suggestSimilar(path); len(s) > 0 {
				msg += "\nDid you mean one of these?\n  - " + strings.Join(s, "\n  - ")
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	if isBinary(data) {
		return nil, fmt.Errorf("[%s#ERR] binary file", filepath.Base(path))
	}
	return data, nil
}

func formatReadOutput(ctx context.Context, path string, lines []string, startLine, endLine int) string {
	total := len(lines)
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		return fmt.Sprintf("[%s#ERR] endLine (%d) < startLine (%d)", filepath.Base(path), endLine, startLine)
	}
	if startLine > total {
		return fmt.Sprintf("[%s#ERR] startLine %d out of range (total %d)", filepath.Base(path), startLine, total)
	}
	start := startLine - 1
	count := min(endLine-startLine+1, maxReadLines)
	end := min(start+count, total)

	fullText := strings.Join(lines, "\n")
	execution.RecordSnapshotInContext(ctx, path, fullText)
	tag := toolutil.ComputeFileHash(fullText)

	var out strings.Builder
	fmt.Fprintf(&out, "[%s#%s]\n", path, tag)
	for i := range end - start {
		idx := start + i
		lineNo := startLine + i
		fmt.Fprintf(&out, "%d:%s\n", lineNo, lines[idx])
	}
	if end < total {
		fmt.Fprintf(&out, "... (next startLine=%d, total=%d)", end+1, total)
	}
	return strings.TrimRight(out.String(), "\n")
}

func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if slices.Contains(data[:min(len(data), 8192)], byte(0)) {
		return true
	}
	return !utf8.Valid(data[:min(len(data), 65536)])
}
