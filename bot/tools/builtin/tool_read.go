package builtin

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"nekocode/bot/tools"
	"nekocode/bot/tools/hashline"
)

type ReadTool struct {
	SafeReadOnlyTool
}

func (t *ReadTool) Name() string { return "read" }
func (t *ReadTool) Description() string {
	return "Read file contents (text, images, PDF). Absolute path required. Use startLine/endLine for range, max 500 lines. Output format: [path#TAG] header followed by lineNo:content lines. TAG is an 8-hex content hash for the edit tool."
}

func (t *ReadTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "File path (absolute)"},
		{Name: "startLine", Type: "integer", Required: true, Description: "First line to read (1-based)"},
		{Name: "endLine", Type: "integer", Required: true, Description: "Last line to read (inclusive, >= startLine)"},
	}
}

const maxReadLines = 500

func (t *ReadTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := requireStringArg(args, "path")
	if err != nil {
		return "", err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return t.readImage(path)
	case ".pdf":
		return t.readPDF(path)
	default:
		return t.readTextCached(path, args)
	}
}

func (t *ReadTool) readTextCached(path string, args map[string]any) (string, error) {
	startLine, err := requireIntArg(args, "startLine")
	if err != nil {
		return "", err
	}
	endLine, err := requireIntArg(args, "endLine")
	if err != nil {
		return "", err
	}

	cache := tools.GetGlobalFileCache()
	if cache != nil {
		// Cache has file content — format from cache regardless of whether
		// the range was already covered. Re-formatting is cheap and avoids the
		// stale hint problem when earlier output has been compacted away.
		if lines, ok := cache.Lines(path); ok {
			result := formatReadOutput(path, lines, startLine, endLine)
			end := startLine + min(endLine-startLine+1, maxReadLines) - 1
			if end > len(lines) {
				end = len(lines)
			}
			cache.Put(path, lines, startLine, end)
			return result, nil
		}
	}

	// First read: read full file into memory, cache it, format output.
	fullLines, err := readFileLines(path)
	if err != nil {
		return "", err
	}

	result := formatReadOutput(path, fullLines, startLine, endLine)
	if cache != nil {
		end := startLine + min(endLine-startLine+1, maxReadLines) - 1
		if end > len(fullLines) {
			end = len(fullLines)
		}
		cache.Put(path, fullLines, startLine, end)
	}
	return result, nil
}

// readFileLines reads a text file and returns its lines, or nil if binary/empty.
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
		// Empty file is valid — return a single empty line.
		return []string{""}, nil
	}
	return lines, nil
}

// formatReadOutput formats lines[startLine:endLine] as the hashline output.
// Format: [path#TAG] header, then lineNo:content lines.
func formatReadOutput(path string, lines []string, startLine, endLine int) string {
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

	// Record full-file snapshot for hashline edit recovery.
	fullText := strings.Join(lines, "\n")
	tools.RecordSnapshot(path, fullText)

	// Compute file hash tag.
	tag := hashline.ComputeFileHash(fullText)

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

func (t *ReadTool) readImage(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open image: %w", err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[Image] %s — %s, %dx%d", filepath.Base(path), format, cfg.Width, cfg.Height), nil
}

func (t *ReadTool) readPDF(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[PDF] %s — %.1fKB. Use pdftotext to extract content.",
		filepath.Base(path), float64(st.Size())/1024), nil
}

func suggestSimilar(path string) []string {
	dir := filepath.Dir(path)
	base := strings.ToLower(filepath.Base(path))
	entries, _ := os.ReadDir(dir)

	type match struct {
		path  string
		score int
	}
	var matches []match
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if name == base {
			continue
		}
		score := 0
		if strings.Contains(name, base) || strings.Contains(base, name) {
			score = 10
		} else if d := levenshtein(name, base); d <= 3 {
			score = max(0, 8-d)
		}
		if score > 0 {
			matches = append(matches, match{filepath.Join(dir, e.Name()), score})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	if len(matches) > 3 {
		matches = matches[:3]
	}
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.path
	}
	return out
}

func levenshtein(a, b string) int {
	m, n := len(a), len(b)
	if m == 0 {
		return n
	}
	if n == 0 {
		return m
	}
	prev, cur := make([]int, n+1), make([]int, n+1)
	for j := range cur {
		prev[j] = j
	}
	for i := 1; i <= m; i++ {
		cur[0] = i
		for j := 1; j <= n; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[n]
}

