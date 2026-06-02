// EditTool — hashline-anchored file editing.

package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"nekocode/bot/tools"

	"nekocode/common"
)

// ---------------------------------------------------------------------------
// tool definition
// ---------------------------------------------------------------------------

type EditTool struct{}

func (t *EditTool) Name() string                                     { return "edit" }
func (t *EditTool) ExecutionMode(map[string]interface{}) tools.ExecutionMode { return tools.ModeSequential }
func (t *EditTool) DangerLevel(args map[string]any) common.DangerLevel {
	hashes, ok := toStringSlice(args["hashes"])
	if !ok || len(hashes) == 0 {
		return common.LevelWrite
	}
	path, _ := args["path"].(string)
	content, err := os.ReadFile(path)
	if err != nil {
		return common.LevelWrite
	}
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	newStr, _ := args["new_string"].(string)
	op, _ := args["op"].(string)

	hashToLines := buildHashToLines(lines)

	startLine := resolveLine(hashToLines, hashes[0])
	if startLine == 0 {
		return common.LevelWrite
	}
	endLine := startLine
	if len(hashes) > 1 {
		endLine = resolveLine(hashToLines, hashes[1])
		if endLine == 0 {
			return common.LevelWrite
		}
	}
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}

	var sb strings.Builder
	sb.WriteString("── preview ──")
	ctxStart := max(0, startLine-4)
	for i := ctxStart; i < startLine-1 && i < len(lines); i++ {
		sb.WriteString(fmt.Sprintf("\n %d:[%s]%s", i+1, tools.HashLine(lines[i]), lines[i]))
	}
	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		sb.WriteString(fmt.Sprintf("\n-%d:[%s]%s", i+1, tools.HashLine(lines[i]), lines[i]))
	}
	if op != "delete" && newStr != "" {
		lineNo := startLine
		if op == "insert_after" {
			lineNo = endLine + 1
		}
		for _, nl := range strings.Split(newStr, "\n") {
			sb.WriteString(fmt.Sprintf("\n+%d:[%s]%s", lineNo, tools.HashLine(nl), nl))
			lineNo++
		}
	}
	ctxEnd := min(len(lines), endLine+3)
	for i := endLine; i < ctxEnd && i < len(lines); i++ {
		sb.WriteString(fmt.Sprintf("\n %d:[%s]%s", i+1, tools.HashLine(lines[i]), lines[i]))
	}
	args["_preview"] = sb.String()
	return common.LevelWrite
}

func (t *EditTool) Description() string {
	return "Edit files by hashline. Read output annotates each line as lineNo:[hash]content — [hash] is metadata, only content after ] is the actual file. Reference lines by hashes like {\"hashes\":[\"3:a3\"]} — use ONLY the 3-char hash, never include [ ] or content. Use 2 hashes for ranges [\"3:a3\",\"5:b2\"]. Operations: replace(default), insert_after, insert_before, delete. Prefer this over write. Always read the file first."
}

func (t *EditTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true,
			Description: "Absolute file path to edit."},
		{Name: "hashes", Type: "array", Required: true,
			Description: "Line anchors: \"lineNo:hash\" from Read output. Use ONLY the 3-char hash (e.g. \"3:a3\"), never include brackets or pipe. e.g. [\"3:a3\",\"5:b2\"]"},
		{Name: "new_string", Type: "string", Required: false,
			Description: "Replacement text (omit for delete)."},
		{Name: "op", Type: "string", Required: false,
			Description: "replace | insert_after | insert_before | delete (default: replace)."},
	}
}

// ---------------------------------------------------------------------------
// execute
// ---------------------------------------------------------------------------

func (t *EditTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("filePath is required")
	}
	safePath, err := tools.ValidatePath(path)
	if err != nil {
		return "", err
	}
	hashes, ok := toStringSlice(args["hashes"])
	if !ok || len(hashes) == 0 {
		return "", fmt.Errorf("hashes is required — read the file first to get line hashes")
	}

	return t.hashlineEdit(safePath, hashes, args)
}

func lintFile(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return lintGo(path)
	default:
		return ""
	}
}

func lintGo(path string) string {
	cmd := exec.Command("gofmt", "-e", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Sprintf("gofmt: %s", msg)
		}
	}
	return ""
}

func toStringSlice(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

func (t *EditTool) hashlineEdit(path string, hashes []string, args map[string]any) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")

	hashToLines := buildHashToLines(lines)

	var stale []string
	for _, h := range hashes {
		if resolveLine(hashToLines, h) == 0 {
			stale = append(stale, h)
		}
	}
	if len(stale) > 0 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "<path>%s</path>\n<error>Hashline stale</error>\n<stale>\n", filepath.Base(path))
		for _, h := range stale {
			fmt.Fprintf(&sb, "  %s\n", h)
		}
		sb.WriteString("</stale>\n<current>\n<format>lineNo:[hash]content — [hash] is NOT file content</format>\n<![CDATA[\n")
		sb.WriteString(tools.AnnotateLines(strings.Join(lines, "\n")))
		sb.WriteString("\n]]>\n</current>")
		return "", fmt.Errorf("%s", sb.String())
	}

	startLine := resolveLine(hashToLines, hashes[0])
	endLine := startLine
	if len(hashes) > 1 {
		endLine = resolveLine(hashToLines, hashes[1])
	}
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}

	op, _ := args["op"].(string)
	newStr, _ := args["new_string"].(string)
	var replacement []string
	if newStr != "" {
		replacement = strings.Split(newStr, "\n")
	}

	switch op {
	case "insert_before":
		lines = append(lines[:startLine-1], append(replacement, lines[startLine-1:]...)...)
	case "insert_after":
		lines = append(lines[:endLine], append(replacement, lines[endLine:]...)...)
	case "delete":
		lines = append(lines[:startLine-1], lines[endLine:]...)
	default:
		lines = append(lines[:startLine-1], append(replacement, lines[endLine:]...)...)
	}

	newText := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(newText), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %v", err)
	}

	_ = lintFile(path)

	what := "Replaced"
	n := len(replacement)
	if op == "insert_after" || op == "insert_before" {
		what = "Inserted"
	} else if op == "delete" {
		what, n = "Deleted", endLine-startLine+1
	}
	return fmt.Sprintf("<path>%s</path>\n%s %d line(s) in %s", filepath.Base(path), what, n, filepath.Base(path)), nil
}

// ---------------------------------------------------------------------------
// hashline helpers
// ---------------------------------------------------------------------------

// cleanHash strips annotation syntax from a hash value: brackets [a3] → a3,
// and legacy separators a3| or a3│ → a3.
func cleanHash(s string) string {
	return strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"), "│|")
}

// parseHashParam splits a hashline param "lineNo:hash" into its parts.
func parseHashParam(s string) (lineNo int, hash string) {
	if idx := strings.IndexByte(s, ':'); idx > 0 {
		if n, err := strconv.Atoi(s[:idx]); err == nil {
			return n, cleanHash(s[idx+1:])
		}
	}
	return 0, cleanHash(s)
}

// buildHashToLines maps each line's hash to its 1-based line numbers.
func buildHashToLines(lines []string) map[string][]int {
	m := make(map[string][]int)
	for i, line := range lines {
		h := tools.HashLine(line)
		m[h] = append(m[h], i+1)
	}
	return m
}

// resolveLine finds the best-matching line number for a hashline param.
// Returns 0 if the hash is not found in the file.
func resolveLine(hashToLines map[string][]int, param string) int {
	expNo, h := parseHashParam(param)
	occ := hashToLines[h]
	if len(occ) == 0 {
		return 0
	}
	if len(occ) == 1 || expNo == 0 {
		return occ[0]
	}
	best := occ[0]
	for _, o := range occ[1:] {
		if abs(o-expNo) < abs(best-expNo) {
			best = o
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
