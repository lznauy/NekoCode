package read

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"nekocode/bot/tools"
	"nekocode/bot/tools/editcore"
	"nekocode/bot/tools/toolhelpers"
)

func (t *ReadTool) readTextCached(ctx context.Context, path string, args map[string]any) (string, error) {
	startLine, err := toolhelpers.RequireIntArg(args, "startLine")
	if err != nil {
		return "", err
	}
	endLine, err := toolhelpers.RequireIntArg(args, "endLine")
	if err != nil {
		return "", err
	}

	cache := tools.FileCacheFromContext(ctx)
	if cache != nil {
		if lines, ok := cache.Lines(path); ok {
			result := formatReadOutput(ctx, path, lines, startLine, endLine)
			end := startLine + min(endLine-startLine+1, maxReadLines) - 1
			if end > len(lines) {
				end = len(lines)
			}
			cache.Put(path, lines, startLine, end)
			return result, nil
		}
	}

	fullLines, err := readFileLines(path)
	if err != nil {
		return "", err
	}

	result := formatReadOutput(ctx, path, fullLines, startLine, endLine)
	if cache != nil {
		end := startLine + min(endLine-startLine+1, maxReadLines) - 1
		if end > len(fullLines) {
			end = len(fullLines)
		}
		cache.Put(path, fullLines, startLine, end)
	}
	return result, nil
}

func readFileLines(path string) ([]string, error) {
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
	text := tools.NormalizeText(string(data))
	lines := strings.Split(text, "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{""}, nil
	}
	return lines, nil
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
	tools.RecordSnapshotInContext(ctx, path, fullText)
	tag := editcore.ComputeFileHash(fullText)
	view := tools.FileView{}
	if store := tools.ViewStoreFromContext(ctx); store != nil {
		view = store.Register(path, lines, startLine, end)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "[%s#%s]\n", path, tag)
	if view.WindowID != "" {
		fmt.Fprintf(&out, "VIEW rev=%s window=%s lines=%d..%d total=%d\n", view.Revision, view.WindowID, view.StartLine, view.EndLine, view.TotalLines)
	}
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
