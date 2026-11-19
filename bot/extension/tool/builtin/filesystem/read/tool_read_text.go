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
	utilfs "nekocode/util/fs"
)

const maxReadFileBytes = 64 << 20

func (t *ReadTool) readTextCached(ctx context.Context, path string, args map[string]any) (core.ToolOutput, error) {
	startLine, err := toolutil.OptIntArg(args, "startLine", 1)
	if err != nil {
		return core.ToolOutput{}, err
	}
	startLine = max(1, startLine)
	// Formatting clamps to the file length and maxReadLines before arithmetic.
	endLine, err := toolutil.OptIntArg(args, "endLine", int(^uint(0)>>1))
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
	data, err := readFileData(ctx, path)
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
	if startLine > len(fullLines) {
		return core.ToolOutput{}, fmt.Errorf("startLine %d is past the end of the file; choose a line from 1 to %d", startLine, len(fullLines))
	}
	if endLine < startLine {
		return core.ToolOutput{}, fmt.Errorf("endLine %d must be at least startLine %d", endLine, startLine)
	}
	result := formatReadOutput(ctx, path, fullLines, startLine, endLine)
	if cache != nil {
		cache.Put(path, fullLines, version)
	}
	return core.ToolOutput{Text: result, ReadID: execution.FileReadsFromContext(ctx).Record(path, data, result)}, nil
}

func readFileData(ctx context.Context, path string) ([]byte, error) {
	data, err := utilfs.ReadRegularFile(ctx, path, maxReadFileBytes)
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
	count := min(min(endLine, total)-startLine+1, maxReadLines)
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
	if end < min(endLine, total) {
		fmt.Fprintf(&out, "... (next startLine=%d, total=%d)", end+1, total)
	} else if end < total {
		fmt.Fprintf(&out, "(requested range complete; total=%d)", total)
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
	end := min(len(data), 65536)
	// Include the rest of a rune crossing the sample boundary. A short file's
	// incomplete final rune must still be rejected as invalid UTF-8.
	for end < min(len(data), 65536+utf8.UTFMax-1) && !utf8.RuneStart(data[end]) {
		end++
	}
	return !utf8.Valid(data[:end])
}
