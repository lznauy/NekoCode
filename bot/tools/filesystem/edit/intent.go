package edit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nekocode/bot/tools"
	"nekocode/bot/tools/diff"
	"nekocode/bot/tools/editcore"
	"nekocode/bot/tools/toolhelpers"
)

type editRequest struct {
	Path       string
	OldString  string
	NewString  string
	ReplaceAll bool
}

type editPlan struct {
	SafePath         string
	NormalizedBefore string
	NormalizedAfter  string
	LineEnding       string
	OrigMode         os.FileMode
	Hunks            []editHunk
	MatchKind        string
	ReplaceAll       bool
}

type editHunk struct {
	OldStart int
	OldEnd   int
	NewStart int
	NewEnd   int
	OldLines []string
	NewLines []string
}

type textMatch struct {
	Start int
	End   int
	Kind  string
}

type lineSpan struct {
	StartByte int
	EndByte   int
	Lines     []string
}

var fileLocks sync.Map

func (t *EditTool) previewEdit(args map[string]any) string {
	plan, err := buildEditPlan(args)
	if err != nil {
		path, _ := args["path"].(string)
		return fmt.Sprintf("(%s: %v)", filepath.Base(path), err)
	}
	preview := diff.RenderTextChange(plan.NormalizedBefore, plan.NormalizedAfter, editTextChangeOptions)
	if plan.ReplaceAll {
		preview = fmt.Sprintf("(%d replacements)\n%s", len(plan.Hunks), preview)
	}
	return appendStructuredDiff(preview, plan.SafePath)
}

func (t *EditTool) executeEdit(ctx context.Context, args map[string]any) (string, error) {
	req, err := parseEditRequest(args)
	if err != nil {
		return "", err
	}
	safePath, err := tools.ValidatePath(req.Path)
	if err != nil {
		return "", err
	}
	lock := lockForPath(safePath)
	lock.Lock()
	defer lock.Unlock()

	args["path"] = safePath
	plan, err := buildEditPlan(args)
	if err != nil {
		return "", err
	}
	pe := preflightResult{
		safePath:         plan.SafePath,
		normalizedBefore: plan.NormalizedBefore,
		lineEnding:       plan.LineEnding,
	}
	writeUndoSnapshot(pe)
	finalText := editcore.RestoreLineEndings(plan.NormalizedAfter, plan.LineEnding)
	if err := os.WriteFile(plan.SafePath, []byte(finalText), plan.OrigMode); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	if cache := tools.FileCacheFromContext(ctx); cache != nil {
		cache.Invalidate(plan.SafePath)
	}
	newTag := tools.RecordSnapshotInContext(ctx, plan.SafePath, plan.NormalizedAfter)
	msg := formatEditResult(plan.SafePath, plan.NormalizedBefore, plan.NormalizedAfter, plan.Hunks, newTag)
	if plan.ReplaceAll {
		msg += fmt.Sprintf("\n(%d replacements)", len(plan.Hunks))
	}
	if plan.MatchKind != "exact" {
		msg += fmt.Sprintf("\n(matched via %s)", plan.MatchKind)
	}
	if lint := lintFile(plan.SafePath); lint != "" {
		msg += "\n" + lint
	}
	return msg, nil
}

func buildEditPlan(args map[string]any) (editPlan, error) {
	req, err := parseEditRequest(args)
	if err != nil {
		return editPlan{}, err
	}
	safePath, err := tools.ValidatePath(req.Path)
	if err != nil {
		return editPlan{}, err
	}
	data, err := tools.ReadSafeFile(safePath)
	if err != nil {
		return editPlan{}, fmt.Errorf("failed to read file: %w", err)
	}
	rawText := string(data)
	lineEnding := editcore.DetectLineEnding(rawText)
	before := editcore.NormalizeToLF(rawText)
	oldString := editcore.NormalizeToLF(req.OldString)
	newString := editcore.NormalizeToLF(req.NewString)

	matches, err := findMatches(before, oldString, req.ReplaceAll)
	if err != nil {
		return editPlan{}, err
	}
	after, hunks := applyMatches(before, newString, matches)
	return editPlan{
		SafePath:         safePath,
		NormalizedBefore: before,
		NormalizedAfter:  after,
		LineEnding:       lineEnding,
		OrigMode:         toolhelpers.GetFileMode(safePath),
		Hunks:            hunks,
		MatchKind:        matches[0].Kind,
		ReplaceAll:       req.ReplaceAll,
	}, nil
}

func parseEditRequest(args map[string]any) (editRequest, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return editRequest{}, fmt.Errorf("path parameter is required")
	}
	oldString, _ := args["oldString"].(string)
	if oldString == "" {
		return editRequest{}, fmt.Errorf("oldString parameter is required")
	}
	newString, _ := args["newString"].(string)
	replaceAll, _ := args["replaceAll"].(bool)
	return editRequest{Path: path, OldString: oldString, NewString: newString, ReplaceAll: replaceAll}, nil
}

func findMatches(text, oldString string, replaceAll bool) ([]textMatch, error) {
	if oldString == "" {
		return nil, fmt.Errorf("oldString parameter is required")
	}
	exact := exactMatches(text, oldString)
	if replaceAll {
		if len(exact) == 0 {
			return nil, fmt.Errorf("oldString was not found")
		}
		return exact, nil
	}
	switch len(exact) {
	case 1:
		return exact, nil
	case 0:
		return fallbackMatches(text, oldString)
	default:
		return nil, ambiguousMatchError(text, exact, "oldString", true)
	}
}

func exactMatches(text, needle string) []textMatch {
	var matches []textMatch
	offset := 0
	for {
		idx := strings.Index(text[offset:], needle)
		if idx < 0 {
			break
		}
		start := offset + idx
		matches = append(matches, textMatch{Start: start, End: start + len(needle), Kind: "exact"})
		offset = start + len(needle)
	}
	return matches
}

func fallbackMatches(text, oldString string) ([]textMatch, error) {
	candidates := []string{
		strings.Trim(oldString, "\n"),
		strings.TrimSpace(oldString),
	}
	seen := map[string]bool{oldString: true}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		matches := exactMatches(text, candidate)
		if len(matches) == 1 {
			matches[0].Kind = "trimmed-exact"
			return matches, nil
		}
		if len(matches) > 1 {
			return nil, ambiguousMatchError(text, matches, "oldString fallback", false)
		}
	}
	matches := trimLineMatches(text, oldString)
	switch len(matches) {
	case 1:
		return matches, nil
	case 0:
		return nil, fmt.Errorf("oldString was not found")
	default:
		return nil, ambiguousMatchError(text, matches, "oldString fallback", false)
	}
}

func ambiguousMatchError(text string, matches []textMatch, label string, mentionReplaceAll bool) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s matched %d times:", label, len(matches))
	limit := min(len(matches), 3)
	for i := 0; i < limit; i++ {
		lineNo := lineNumberAtOffset(text, matches[i].Start)
		fmt.Fprintf(&sb, "\n- line %d: %s", lineNo, truncateMatchContext(lineAtOffset(text, matches[i].Start), 120))
	}
	if len(matches) > limit {
		fmt.Fprintf(&sb, "\n- ... %d more matches", len(matches)-limit)
	}
	sb.WriteString("\nInclude more surrounding context to make oldString unique")
	if mentionReplaceAll {
		sb.WriteString(", or set replaceAll=true")
	}
	sb.WriteString(".")
	return fmt.Errorf("%s", sb.String())
}

func trimLineMatches(text, oldString string) []textMatch {
	spans := splitLineSpans(text)
	needleLines := diff.SplitLines(strings.Trim(oldString, "\n"))
	if len(needleLines) == 0 || len(needleLines) > len(spans) {
		return nil
	}
	for i, line := range needleLines {
		needleLines[i] = strings.TrimSpace(line)
	}
	var matches []textMatch
	for start := 0; start+len(needleLines) <= len(spans); start++ {
		ok := true
		for i := range needleLines {
			if strings.TrimSpace(spans[start+i].Lines[0]) != needleLines[i] {
				ok = false
				break
			}
		}
		if ok {
			last := spans[start+len(needleLines)-1]
			matches = append(matches, textMatch{
				Start: spans[start].StartByte,
				End:   last.EndByte,
				Kind:  "line-trim",
			})
		}
	}
	return matches
}

func applyMatches(text, replacement string, matches []textMatch) (string, []editHunk) {
	var out strings.Builder
	hunks := make([]editHunk, 0, len(matches))
	cursor := 0
	lineDelta := 0
	for _, m := range matches {
		out.WriteString(text[cursor:m.Start])
		oldSegment := text[m.Start:m.End]
		effectiveReplacement := replacementForMatch(replacement, oldSegment, m)
		oldStart := lineNumberAtOffset(text, m.Start)
		oldLines := diff.SplitLines(oldSegment)
		newLines := diff.SplitLines(effectiveReplacement)
		newStart := oldStart + lineDelta
		hunks = append(hunks, editHunk{
			OldStart: oldStart,
			OldEnd:   oldStart + max(len(oldLines), 1) - 1,
			NewStart: newStart,
			NewEnd:   newStart + max(len(newLines), 1) - 1,
			OldLines: oldLines,
			NewLines: newLines,
		})
		out.WriteString(effectiveReplacement)
		cursor = m.End
		lineDelta += len(newLines) - len(oldLines)
	}
	out.WriteString(text[cursor:])
	return out.String(), hunks
}

func replacementForMatch(replacement, oldSegment string, match textMatch) string {
	if match.Kind == "line-trim" && strings.HasSuffix(oldSegment, "\n") && !strings.HasSuffix(replacement, "\n") {
		return replacement + "\n"
	}
	return replacement
}

func splitLineSpans(text string) []lineSpan {
	if text == "" {
		return []lineSpan{{StartByte: 0, EndByte: 0, Lines: []string{""}}}
	}
	parts := strings.SplitAfter(text, "\n")
	spans := make([]lineSpan, 0, len(parts))
	offset := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		line := strings.TrimSuffix(part, "\n")
		spans = append(spans, lineSpan{
			StartByte: offset,
			EndByte:   offset + len(part),
			Lines:     []string{line},
		})
		offset += len(part)
	}
	return spans
}

func lineNumberAtOffset(text string, offset int) int {
	if offset <= 0 {
		return 1
	}
	if offset > len(text) {
		offset = len(text)
	}
	return strings.Count(text[:offset], "\n") + 1
}

func lineAtOffset(text string, offset int) string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := strings.LastIndex(text[:offset], "\n") + 1
	endRel := strings.Index(text[offset:], "\n")
	if endRel < 0 {
		return text[start:]
	}
	return text[start : offset+endRel]
}

func truncateMatchContext(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func lockForPath(path string) *sync.Mutex {
	actual, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	return actual.(*sync.Mutex)
}
