// EditTool — cascading-replacer file editing.
// Design copied from opencode: 9 replacer strategies tried in priority order,
// from exact to fuzzy. First replacer yielding a unique match wins.
// Line endings normalized before matching. Per-file mutex prevents races.

package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"nekocode/bot/tools"

	"nekocode/common"
)

// ---------------------------------------------------------------------------
// tool definition
// ---------------------------------------------------------------------------

type EditTool struct{}

func (t *EditTool) Name() string                                     { return "edit" }
func (t *EditTool) ExecutionMode(map[string]interface{}) tools.ExecutionMode { return tools.ModeSequential }
func (t *EditTool) DangerLevel(args map[string]interface{}) common.DangerLevel   { return common.LevelWrite }

func (t *EditTool) Description() string {
	return "Exact string replacement in files. old_string must match exactly (copy from Read output — tabs, spaces, everything). Fails if not unique (use replace_all=true for multiple matches). Prefer over Write for partial changes."
}

func (t *EditTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true,
			Description: "Absolute file path to edit."},
		{Name: "old_string", Type: "string", Required: true,
			Description: "Exact text to replace — copy verbatim from Read output. Must be unique in the file."},
		{Name: "new_string", Type: "string", Required: true,
			Description: "Replacement text. Must differ from old_string."},
		{Name: "replace_all", Type: "boolean", Required: false,
			Description: "Replace ALL occurrences (default false). Set true for renaming."},
	}
}

// ---------------------------------------------------------------------------
// execute
// ---------------------------------------------------------------------------

var fileLocks sync.Map // path → *sync.Mutex

func (t *EditTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("filePath is required")
	}
	safePath, err := tools.ValidatePath(path)
	if err != nil {
		return "", err
	}
	oldStr, ok := args["old_string"].(string)
	if !ok || oldStr == "" {
		return "", fmt.Errorf("old_string is required")
	}
	newStr, _ := args["new_string"].(string)
	if oldStr == newStr {
		return "", fmt.Errorf("No changes to apply: oldString and newString are identical.")
	}
	replaceAll := false
	if v, ok := args["replace_all"].(bool); ok {
		replaceAll = v
	}

	// Per-file lock.
	muI, _ := fileLocks.LoadOrStore(safePath, &sync.Mutex{})
	mu := muI.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// Read existing file (or create if empty oldStr).
	if oldStr == "" {
		return t.createFile(safePath, newStr)
	}
	info, err := os.Stat(safePath)
	if err != nil {
		if os.IsNotExist(err) {
			msg := fmt.Sprintf("File not found: %s", filepath.Base(safePath))
			if suggestions := suggestSimilar(safePath); len(suggestions) > 0 {
				msg += "\nDid you mean one of these?"
				for _, s := range suggestions {
					msg += "\n  - " + s
				}
			}
			return "", fmt.Errorf("%s", msg)
		}
		return "", fmt.Errorf("failed to stat file: %v", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("Path is a directory, not a file: %s", safePath)
	}
	content, err := os.ReadFile(safePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}
	original := string(content)

	// Normalize line endings: \r\n → \n for matching, preserve original
	// when writing back.
	ending := detectLineEnding(original)
	normContent := normalizeLineEndings(original)
	normOld := convertToLineEnding(normalizeLineEndings(oldStr), ending)
	normNew := convertToLineEnding(normalizeLineEndings(newStr), ending)

	replaced, err := replace(normContent, normOld, normNew, replaceAll)
	if err != nil {
		return "", err
	}
	if replaced == normContent {
		return "", fmt.Errorf("No changes to apply: oldString and newString are identical.")
	}

	// Write back.
	dir := filepath.Dir(safePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(safePath, []byte(replaced), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %v", err)
	}

	// Build structured output.
	count := 1
	if replaceAll {
		count = countOccurrences(normContent, normOld)
	}
	return buildEditOutput(filepath.Base(safePath), normOld, normNew, count, safePath), nil
}

func (t *EditTool) createFile(path, content string) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %v", err)
	}
	return buildCreateOutput(filepath.Base(path), content), nil
}

// ---------------------------------------------------------------------------
// line ending utilities
// ---------------------------------------------------------------------------

func normalizeLineEndings(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
func detectLineEnding(s string) string {
	if strings.Contains(s, "\r\n") {
		return "\r\n"
	}
	return "\n"
}
func convertToLineEnding(s, ending string) string {
	if ending == "\n" {
		return s
	}
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// ---------------------------------------------------------------------------
// replacer type & cascade
// ---------------------------------------------------------------------------

// A replacer yields candidate substrings from content that match find.
type replacer func(content, find string) []string

func replace(content, oldString, newString string, replaceAll bool) (string, error) {
	if oldString == newString {
		return content, fmt.Errorf("No changes to apply: oldString and newString are identical.")
	}
	replacers := []replacer{
		simpleReplacer,
		lineTrimmedReplacer,
		blockAnchorReplacer,
		whitespaceNormalizedReplacer,
		indentationFlexibleReplacer,
		escapeNormalizedReplacer,
		trimmedBoundaryReplacer,
		contextAwareReplacer,
		multiOccurrenceReplacer,
	}
	for _, repl := range replacers {
		for _, search := range repl(content, oldString) {
			idx := strings.Index(content, search)
			if idx == -1 {
				continue
			}
			if replaceAll {
				return strings.ReplaceAll(content, search, newString), nil
			}
			lastIdx := strings.LastIndex(content, search)
			if idx != lastIdx {
				continue // ambiguous, try next replacer / candidate
			}
			return content[:idx] + newString + content[idx+len(search):], nil
		}
	}
	return content, fmt.Errorf("Could not find oldString in the file. It must match exactly, including whitespace, indentation, and line endings.")
}

// ---------------------------------------------------------------------------
// replacer implementations (order = priority)
// ---------------------------------------------------------------------------

// 1. simpleReplacer — exact substring match.
func simpleReplacer(_, find string) []string { return []string{find} }

// 2. lineTrimmedReplacer — trim each line, match line-by-line, yield original substring.
func lineTrimmedReplacer(content, find string) []string {
	origLines := strings.Split(content, "\n")
	searchLines := strings.Split(find, "\n")
	if searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	var results []string
	for i := 0; i <= len(origLines)-len(searchLines); i++ {
		matches := true
		for j := 0; j < len(searchLines); j++ {
			if strings.TrimSpace(origLines[i+j]) != strings.TrimSpace(searchLines[j]) {
				matches = false
				break
			}
		}
		if matches {
			start := 0
			for k := 0; k < i; k++ {
				start += len(origLines[k]) + 1
			}
			end := start
			for k := 0; k < len(searchLines); k++ {
				end += len(origLines[i+k])
				if k < len(searchLines)-1 {
					end++
				}
			}
			results = append(results, content[start:end])
		}
	}
	return results
}

// 3. blockAnchorReplacer — first+last line as anchors, Levenshtein similarity on middle.
const (
	singleCandidateThreshold   = 0.0
	multipleCandidateThreshold = 0.3
)

func blockAnchorReplacer(content, find string) []string {
	origLines := strings.Split(content, "\n")
	searchLines := strings.Split(find, "\n")
	if len(searchLines) < 3 {
		return nil
	}
	if searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	firstSearch := strings.TrimSpace(searchLines[0])
	lastSearch := strings.TrimSpace(searchLines[len(searchLines)-1])
	searchBlockSize := len(searchLines)

	type candidate struct{ startLine, endLine int }
	var candidates []candidate
	for i := 0; i < len(origLines); i++ {
		if strings.TrimSpace(origLines[i]) != firstSearch {
			continue
		}
		for j := i + 2; j < len(origLines); j++ {
			if strings.TrimSpace(origLines[j]) == lastSearch {
				candidates = append(candidates, candidate{i, j})
				break
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) == 1 {
		c := candidates[0]
		actualBlockSize := c.endLine - c.startLine + 1
		similarity := 1.0
		linesToCheck := minInt(searchBlockSize-2, actualBlockSize-2)
		if linesToCheck > 0 {
			similarity = 0
			for j := 1; j < searchBlockSize-1 && j < actualBlockSize-1; j++ {
				orig := strings.TrimSpace(origLines[c.startLine+j])
				search := strings.TrimSpace(searchLines[j])
				maxLen := maxInt(len(orig), len(search))
				if maxLen == 0 {
					continue
				}
				d := levenshteinDistStr(orig, search)
				similarity += (1.0 - float64(d)/float64(maxLen)) / float64(linesToCheck)
			}
		}
		if similarity >= singleCandidateThreshold {
			return []string{blockSubstring(content, origLines, c.startLine, c.endLine)}
		}
		return nil
	}

	bestMatch := candidate{-1, -1}
	maxSimilarity := -1.0
	for _, c := range candidates {
		actualBlockSize := c.endLine - c.startLine + 1
		similarity := 1.0
		linesToCheck := minInt(searchBlockSize-2, actualBlockSize-2)
		if linesToCheck > 0 {
			similarity = 0
			for j := 1; j < searchBlockSize-1 && j < actualBlockSize-1; j++ {
				orig := strings.TrimSpace(origLines[c.startLine+j])
				search := strings.TrimSpace(searchLines[j])
				maxLen := maxInt(len(orig), len(search))
				if maxLen == 0 {
					continue
				}
				d := levenshteinDistStr(orig, search)
				similarity += 1.0 - float64(d)/float64(maxLen)
			}
			similarity /= float64(linesToCheck)
		}
		if similarity > maxSimilarity {
			maxSimilarity = similarity
			bestMatch = c
		}
	}
	if maxSimilarity >= multipleCandidateThreshold && bestMatch.startLine >= 0 {
		return []string{blockSubstring(content, origLines, bestMatch.startLine, bestMatch.endLine)}
	}
	return nil
}

// blockSubstring extracts lines[start] through lines[end] from content,
// computing exact byte offsets by summing line lengths. This guarantees
// the returned string is the exact substring at the matching position,
// so strings.Index(content, result) will always find it.
func blockSubstring(content string, lines []string, start, end int) string {
	pos := 0
	for k := 0; k < start; k++ {
		pos += len(lines[k]) + 1
	}
	endPos := pos
	for k := start; k <= end; k++ {
		endPos += len(lines[k])
		if k < end {
			endPos++
		}
	}
	if endPos > len(content) {
		endPos = len(content)
	}
	return content[pos:endPos]
}

// 4. whitespaceNormalizedReplacer — collapse all whitespace sequences to single space.
func whitespaceNormalizedReplacer(content, find string) []string {
	wsRE := regexp.MustCompile(`\s+`)
	normalize := func(s string) string { return strings.TrimSpace(wsRE.ReplaceAllString(s, " ")) }
	normFind := normalize(find)

	var results []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if normalize(line) == normFind {
			results = append(results, line)
		}
	}
	// Multi-line: try each block.
	findLines := strings.Split(find, "\n")
	if len(findLines) > 1 {
		for i := 0; i <= len(lines)-len(findLines); i++ {
			block := strings.Join(lines[i:i+len(findLines)], "\n")
			if normalize(block) == normFind {
				results = append(results, block)
			}
		}
	}
	return results
}

// 5. indentationFlexibleReplacer — strip common leading indentation from both sides.
func indentationFlexibleReplacer(content, find string) []string {
	removeIndent := func(s string) string {
		lines := strings.Split(s, "\n")
		minIndent := -1
		for _, l := range lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			indent := len(l) - len(strings.TrimLeft(l, " \t"))
			if minIndent < 0 || indent < minIndent {
				minIndent = indent
			}
		}
		if minIndent <= 0 {
			return s
		}
		for i, l := range lines {
			if strings.TrimSpace(l) != "" && len(l) >= minIndent {
				lines[i] = l[minIndent:]
			}
		}
		return strings.Join(lines, "\n")
	}
	normFind := removeIndent(find)
	contentLines := strings.Split(content, "\n")
	findLines := strings.Split(find, "\n")
	var results []string
	for i := 0; i <= len(contentLines)-len(findLines); i++ {
		block := strings.Join(contentLines[i:i+len(findLines)], "\n")
		if removeIndent(block) == normFind {
			results = append(results, block)
		}
	}
	return results
}

// 6. escapeNormalizedReplacer — unescape \n \t \r etc. in find before matching.
func escapeNormalizedReplacer(content, find string) []string {
	unescape := func(s string) string {
		r := strings.NewReplacer(
			`\n`, "\n", `\t`, "\t", `\r`, "\r",
			`\'`, "'", `\"`, `"`, "\\`", "`",
			`\\`, "\\", `\$`, "$",
		)
		return r.Replace(s)
	}
	unescaped := unescape(find)
	var results []string
	if strings.Contains(content, unescaped) {
		results = append(results, unescaped)
	}
	lines := strings.Split(content, "\n")
	findLines := strings.Split(unescaped, "\n")
	for i := 0; i <= len(lines)-len(findLines); i++ {
		block := strings.Join(lines[i:i+len(findLines)], "\n")
		if unescape(block) == unescaped {
			results = append(results, block)
		}
	}
	return results
}

// 7. trimmedBoundaryReplacer — trim leading/trailing whitespace from find.
func trimmedBoundaryReplacer(content, find string) []string {
	trimmed := strings.TrimSpace(find)
	if trimmed == find {
		return nil // already trimmed
	}
	var results []string
	if strings.Contains(content, trimmed) {
		results = append(results, trimmed)
	}
	lines := strings.Split(content, "\n")
	findLines := strings.Split(find, "\n")
	for i := 0; i <= len(lines)-len(findLines); i++ {
		block := strings.Join(lines[i:i+len(findLines)], "\n")
		if strings.TrimSpace(block) == trimmed {
			results = append(results, block)
		}
	}
	return results
}

// 8. contextAwareReplacer — first/last line anchors, >=50% middle match.
func contextAwareReplacer(content, find string) []string {
	findLines := strings.Split(find, "\n")
	if len(findLines) < 3 {
		return nil
	}
	if findLines[len(findLines)-1] == "" {
		findLines = findLines[:len(findLines)-1]
	}
	contentLines := strings.Split(content, "\n")
	firstLine := strings.TrimSpace(findLines[0])
	lastLine := strings.TrimSpace(findLines[len(findLines)-1])

	for i := 0; i < len(contentLines); i++ {
		if strings.TrimSpace(contentLines[i]) != firstLine {
			continue
		}
		for j := i + 2; j < len(contentLines); j++ {
			if strings.TrimSpace(contentLines[j]) != lastLine {
				continue
			}
			blockLines := contentLines[i : j+1]
			if len(blockLines) != len(findLines) {
				break
			}
			matching, total := 0, 0
			for k := 1; k < len(blockLines)-1; k++ {
				bl := strings.TrimSpace(blockLines[k])
				fl := strings.TrimSpace(findLines[k])
				if len(bl) > 0 || len(fl) > 0 {
					total++
					if bl == fl {
						matching++
					}
				}
			}
			if total == 0 || float64(matching)/float64(total) >= 0.5 {
				return []string{strings.Join(blockLines, "\n")}
			}
			break
		}
	}
	return nil
}

// 9. multiOccurrenceReplacer — yield all exact matches (used with replaceAll).
func multiOccurrenceReplacer(content, find string) []string {
	var results []string
	rest := content
	for {
		idx := strings.Index(rest, find)
		if idx == -1 {
			break
		}
		results = append(results, find)
		rest = rest[idx+len(find):]
	}
	return results
}

// ---------------------------------------------------------------------------
// levenshtein distance
// ---------------------------------------------------------------------------

func levenshteinDistStr(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := 0; j <= len(b); j++ {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = minInt(d[i-1][j]+1, minInt(d[i][j-1]+1, d[i-1][j-1]+cost))
		}
	}
	return d[len(a)][len(b)]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// output formatting
// ---------------------------------------------------------------------------

func buildEditOutput(filename, oldStr, newStr string, occurrences int, safePath string) string {
	var b strings.Builder
	b.WriteString("<tool_output name=\"edit\">\n")
	fmt.Fprintf(&b, "<path>%s</path>\n", filename)
	fmt.Fprintf(&b, "<occurrences>%d</occurrences>\n", occurrences)
	b.WriteString("<diff>\n<![CDATA[\n")
	for _, l := range strings.Split(oldStr, "\n") {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	for _, l := range strings.Split(newStr, "\n") {
		fmt.Fprintf(&b, "+ %s\n", l)
	}
	b.WriteString("]]>\n</diff>\n")
	if lintMsg := lintFile(safePath); lintMsg != "" {
		b.WriteString("<lint status=\"error\">\n")
		b.WriteString(lintMsg)
		b.WriteString("\n</lint>\n")
	} else {
		b.WriteString("<lint status=\"ok\"/>\n")
	}
	b.WriteString("</tool_output>")
	return b.String()
}

func buildCreateOutput(filename, content string) string {
	var b strings.Builder
	b.WriteString("<tool_output name=\"edit\">\n")
	fmt.Fprintf(&b, "<path>%s</path>\n", filename)
	b.WriteString("<occurrences>1</occurrences>\n")
	b.WriteString("<diff>\n<![CDATA[\n")
	for _, l := range strings.Split(content, "\n") {
		fmt.Fprintf(&b, "+ %s\n", l)
	}
	b.WriteString("]]>\n</diff>\n")
	b.WriteString("<lint status=\"ok\"/>\n")
	b.WriteString("</tool_output>")
	return b.String()
}

// ---------------------------------------------------------------------------
// lint-after-write
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func countOccurrences(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	n := 0
	rest := s
	for {
		i := strings.Index(rest, substr)
		if i == -1 {
			return n
		}
		n++
		rest = rest[i+len(substr):]
	}
}
