package hashline

import (
	"fmt"
	"sort"
	"strings"
)

// ApplyResult holds the outcome of applying edits.
type ApplyResult struct {
	Text             string
	FirstChangedLine int
	Warnings         []string
	ResolvedHunks    []Hunk
}

// BlockSpan represents the resolved line range of a code block.
type BlockSpan struct {
	Start int // 1-based inclusive
	End   int // 1-based inclusive
}

// BlockResolver resolves a line number to the enclosing code block's span.
type BlockResolver func(path string, line int) (*BlockSpan, error)

// autoprefixSentinel is used by parsePayload to mark bare body rows that
// were auto-prefixed. ApplyEdits strips it and emits a warning.
const autoprefixSentinel = "\x00autoprefix:"

// ApplyEdits applies a list of hunks to the given text and returns the result.
func ApplyEdits(text string, hunks []Hunk, resolver BlockResolver, path string) (*ApplyResult, error) {
	if len(hunks) == 0 {
		return &ApplyResult{Text: text}, nil
	}

	lines := strings.Split(NormalizeToLF(text), "\n")

	// Strip auto-prefix sentinels and collect warnings.
	var autoprefixWarnings []string
	for hi := range hunks {
		for pi := range hunks[hi].Payload {
			if rest, ok := strings.CutPrefix(hunks[hi].Payload[pi], autoprefixSentinel); ok {
				hunks[hi].Payload[pi] = rest
				if len(autoprefixWarnings) == 0 {
					autoprefixWarnings = append(autoprefixWarnings,
						"auto-prefixed bare body row(s) with '+'. Body rows must be '+TEXT' literal lines.")
				}
			}
		}
	}

	// Resolve block hunks to concrete ranges.
	resolved, err := resolveBlockHunks(hunks, lines, resolver, path)
	if err != nil {
		return nil, err
	}
	hunks = resolved

	// Drop delete hunks targeting the trailing newline sentinel — a phantom
	// line that \n-split produces when the file ends with a newline. Deleting
	// it would only strip the file's final newline; the intended operation is
	// always to delete the last concrete line, which inclusive ranges achieve
	// naturally. Mirrors oh-my-pi's dropTrailingPhantomDeletes.
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		phantomLine := len(lines)
		filtered := hunks[:0]
		for _, h := range hunks {
			if h.Kind == HunkDelete && h.Start == phantomLine && h.End == phantomLine {
				continue
			}
			filtered = append(filtered, h)
		}
		hunks = filtered
	}

	resolvedHunks := make([]Hunk, len(hunks))
	copy(resolvedHunks, hunks)

	// Validate all hunk ranges.
	for _, h := range hunks {
		if h.Kind == HunkInsert && (h.Cursor == "head" || h.Cursor == "tail") {
			continue
		}
		if h.Start < 1 || h.Start > len(lines) {
			return nil, fmt.Errorf("hunk start line %d out of range [1..%d]", h.Start, len(lines))
		}
		if h.Kind != HunkInsert && (h.End < 1 || h.End < h.Start || h.End > len(lines)) {
			return nil, fmt.Errorf("hunk end line %d out of range [1..%d]", h.End, len(lines))
		}
	}

	// Sort hunks bottom-up for stable application.
	// Use original index as tiebreaker to ensure stable ordering for
	// head/tail inserts that share the same Start value (0).
	type indexedHunk struct {
		Hunk
		idx int
	}
	indexed := make([]indexedHunk, len(hunks))
	for i, h := range hunks {
		indexed[i] = indexedHunk{h, i}
	}
	sort.Slice(indexed, func(i, j int) bool {
		a, b := indexed[i], indexed[j]
		aHead := a.Kind == HunkInsert && a.Cursor == "head"
		bHead := b.Kind == HunkInsert && b.Cursor == "head"
		aTail := a.Kind == HunkInsert && a.Cursor == "tail"
		bTail := b.Kind == HunkInsert && b.Cursor == "tail"

		// Head inserts sort before everything else.
		if aHead && !bHead {
			return true
		}
		if bHead && !aHead {
			return false
		}
		// Tail inserts sort after everything else.
		if aTail && !bTail {
			return false
		}
		if bTail && !aTail {
			return true
		}
		// Both are same special type (both head or both tail) — preserve
		// patch order using original index as tiebreaker.
		if a.Start == b.Start {
			return a.idx < b.idx
		}
		// For regular hunks, sort descending by Start (bottom-up).
		return a.Start > b.Start
	})
	sorted := make([]Hunk, len(indexed))
	for i, ih := range indexed {
		sorted[i] = ih.Hunk
	}

	// After-insert landing shift: slide after-insert hunks past trailing
	// structural closers when the body indentation is shallower than the anchor.
	sorted, landingWarnings := RepairAfterInsertLandings(sorted, lines)
	autoprefixWarnings = append(autoprefixWarnings, landingWarnings...)

	// Apply hunks.
	firstChanged := len(lines) + 1
	var warnings []string

	for i := range sorted {
		h := &sorted[i]
		switch h.Kind {
		case HunkReplace:
			payload := h.Payload
			payload, w := repairBoundaries(lines, *h, payload)
			warnings = append(warnings, w...)

			start := h.Start - 1
			end := h.End

			payload = repairDelimiterBalance(lines[start:end], payload)

			newLines := make([]string, 0, len(lines)+len(payload)-(end-start))
			newLines = append(newLines, lines[:start]...)
			newLines = append(newLines, payload...)
			newLines = append(newLines, lines[end:]...)
			lines = newLines
			if h.Start < firstChanged {
				firstChanged = h.Start
			}
			// Update the hunk payload to reflect boundary repair so the
			// returned ResolvedHunks match what was actually applied.
			h.Payload = payload

		case HunkDelete:
			start := h.Start - 1
			end := h.End
			lines = append(lines[:start], lines[end:]...)
			if h.Start < firstChanged {
				firstChanged = h.Start
			}

		case HunkInsert:
			var idx int
			switch h.Cursor {
			case "head":
				idx = 0
			case "tail":
				idx = len(lines)
			case "before":
				idx = h.Start - 1
			case "after":
				idx = h.Start
			default:
				idx = h.Start - 1
			}
			if idx < 0 {
				idx = 0
			}
			if idx > len(lines) {
				idx = len(lines)
			}
			newLines := make([]string, 0, len(lines)+len(h.Payload))
			newLines = append(newLines, lines[:idx]...)
			newLines = append(newLines, h.Payload...)
			newLines = append(newLines, lines[idx:]...)
			lines = newLines
			if idx < firstChanged {
				firstChanged = idx + 1
			}
		}
	}

	if firstChanged > len(lines) {
		firstChanged = len(lines)
	}

	warnings = append(autoprefixWarnings, warnings...)

	// Collapse runs of 3+ blank lines to 2 — keeps code tidy.
	lines = collapseExcessBlankLines(lines)

	// Rebuild resolvedHunks from sorted to include boundary repair changes.
	resolvedHunks = make([]Hunk, len(indexed))
	for i, ih := range indexed {
		// Find the hunk in sorted by matching the original index.
		// Since sorted may have different ordering, use the idx field
		// stored in indexed to map back to the original position.
		resolvedHunks[ih.idx] = sorted[i]
	}

	return &ApplyResult{
		Text:             strings.Join(lines, "\n"),
		FirstChangedLine: firstChanged,
		Warnings:         warnings,
		ResolvedHunks:    resolvedHunks,
	}, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// After-insert landing shift
//
// When an "insert after N:" body is shallower than the anchor line, and lines
// below the anchor are structural closers (e.g. "});", "]", ")"), slide the
// landing point past those closers. This catches the common LLM mistake of
// anchoring on the last content line they read instead of after the block.

func RepairAfterInsertLandings(sorted []Hunk, fileLines []string) ([]Hunk, []string) {
	if len(sorted) == 0 {
		return sorted, nil
	}

	// Build set of lines explicitly targeted by any hunk — shifts never cross them.
	targetedLines := make(map[int]bool)
	for _, h := range sorted {
		if h.Kind == HunkDelete {
			for l := h.Start; l <= h.End; l++ {
				targetedLines[l] = true
			}
		} else if h.Kind == HunkInsert && h.Cursor != "head" && h.Cursor != "tail" {
			targetedLines[h.Start] = true
		} else if h.Kind == HunkReplace {
			for l := h.Start; l <= h.End; l++ {
				targetedLines[l] = true
			}
		}
	}

	var warnings []string
	for i := range sorted {
		h := &sorted[i]
		if h.Kind != HunkInsert || h.Cursor != "after" || h.Start < 1 || h.Start > len(fileLines) {
			continue
		}
		target, ok := bodyTargetIndent(h.Payload)
		if !ok {
			continue
		}
		anchorText := fileLines[h.Start-1]
		anchorIndent := leadingIndent(anchorText)
		if !strings.HasPrefix(anchorIndent, target) || len(anchorIndent) <= len(target) {
			continue
		}
		// Anchor is deeper than the body — try to slide past trailing closers.
		landing := h.Start
		crossed := 0
		for line := h.Start + 1; line <= len(fileLines); line++ {
			text := fileLines[line-1]
			if strings.TrimSpace(text) == "" {
				continue
			}
			if !isStructuralCloser(text) {
				break
			}
			indent := leadingIndent(text)
			if !strings.HasPrefix(indent, target) {
				break
			}
			if targetedLines[line] && line != h.Start {
				break
			}
			landing = line
			crossed++
			if indent == target {
				break
			}
		}
		if crossed > 0 {
			origAnchor := h.Start
			h.Start = landing
			warnings = append(warnings, fmt.Sprintf(
				"insert after %d: body indented shallower than anchor, landing shifted past %d closing line(s) to after %d",
				origAnchor, crossed, landing))
		}
	}
	return sorted, warnings
}

func bodyTargetIndent(rows []string) (string, bool) {
	var target string
	first := true
	for _, row := range rows {
		trimmed := strings.TrimSpace(row)
		if trimmed == "" {
			continue
		}
		// A body of pure closers claims no depth.
		if isStructuralCloser(trimmed) {
			continue
		}
		indent := leadingIndent(row)
		if first {
			target = indent
			first = false
			continue
		}
		// Find common prefix of indentation styles.
		if strings.HasPrefix(indent, target) {
			continue
		}
		if strings.HasPrefix(target, indent) {
			target = indent
		} else {
			return "", false
		}
	}
	if first {
		return "", false
	}
	return target, true
}

func leadingIndent(line string) string {
	var end int
	for end < len(line) {
		ch := line[end]
		if ch != '\t' && ch != ' ' {
			break
		}
		end++
	}
	return line[:end]
}

// ═══════════════════════════════════════════════════════════════════════════
// Boundary repair
// ═══════════════════════════════════════════════════════════════════════════

func repairBoundaries(allLines []string, h Hunk, payload []string) ([]string, []string) {
	if len(payload) < 2 {
		return payload, nil
	}
	start := h.Start - 1
	end := h.End

	// Count consecutive duplicate leading lines.
	leadCount := 0
	for leadCount < len(payload) && start-1-leadCount >= 0 {
		if strings.TrimSpace(payload[leadCount]) != strings.TrimSpace(allLines[start-1-leadCount]) ||
			strings.TrimSpace(payload[leadCount]) == "" {
			break
		}
		leadCount++
	}

	// Count consecutive duplicate trailing lines.
	trailCount := 0
	for trailCount < len(payload) && end+trailCount < len(allLines) {
		idx := len(payload) - 1 - trailCount
		if strings.TrimSpace(payload[idx]) != strings.TrimSpace(allLines[end+trailCount]) ||
			strings.TrimSpace(payload[idx]) == "" {
			break
		}
		trailCount++
	}
	// Don't strip everything.
	if leadCount+trailCount >= len(payload) {
		return payload, nil
	}

	var warnings []string
	if leadCount > 0 {
		payload = payload[leadCount:]
		warnings = append(warnings, fmt.Sprintf("stripped %d duplicate leading context line(s)", leadCount))
	}
	if trailCount > 0 {
		payload = payload[:len(payload)-trailCount]
		warnings = append(warnings, fmt.Sprintf("stripped %d duplicate trailing context line(s)", trailCount))
	}
	return payload, warnings
}

// ═══════════════════════════════════════════════════════════════════════════
// Delimiter balance repair
// ═══════════════════════════════════════════════════════════════════════════

func repairDelimiterBalance(deletedLines, payload []string) []string {
	delOpen, delClose := countDelimiters(deletedLines)
	payOpen, payClose := countDelimiters(payload)

	delImbalance := delOpen - delClose
	payImbalance := payOpen - payClose

	missingClose := payImbalance - delImbalance
	if missingClose <= 0 {
		return payload
	}

	closers := extractTrailingClosers(deletedLines)
	if len(closers) == 0 {
		return payload
	}

	appended := 0
	for _, c := range closers {
		if appended >= missingClose {
			break
		}
		payload = append(payload, c)
		appended++
	}
	return payload
}

func countDelimiters(lines []string) (open, close int) {
	inBlockComment := false
	for _, line := range lines {
		o, c, inBlock := countDelimitersInLine(line, inBlockComment)
		open += o
		close += c
		inBlockComment = inBlock
	}
	return
}

func countDelimitersInLine(line string, inBlockComment bool) (open, close int, stillInBlock bool) {
	stillInBlock = inBlockComment
	bs := []byte(line)
	for i := 0; i < len(bs); i++ {
		ch := bs[i]

		if !stillInBlock && i+1 < len(bs) && ch == '/' && bs[i+1] == '/' {
			break
		}
		if !stillInBlock && i+1 < len(bs) && ch == '/' && bs[i+1] == '*' {
			stillInBlock = true
			i++
			continue
		}
		if stillInBlock && i+1 < len(bs) && ch == '*' && bs[i+1] == '/' {
			stillInBlock = false
			i++
			continue
		}
		if stillInBlock {
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			quote := ch
			i++
			for i < len(bs) {
				if bs[i] == '\\' {
					i += 2
					continue
				}
				if bs[i] == quote {
					break
				}
				i++
			}
			continue
		}

		switch ch {
		case '(', '{', '[':
			open++
		case ')', '}', ']':
			close++
		}
	}
	return
}

func extractTrailingClosers(lines []string) []string {
	var closers []string
	for i := len(lines) - 1; i >= 0; i-- {
		if isStructuralCloser(lines[i]) {
			closers = append([]string{lines[i]}, closers...)
		} else {
			break
		}
	}
	return closers
}

func isStructuralCloser(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	hasCloser := false
	for _, ch := range trimmed {
		switch ch {
		case ')', '}', ']':
			hasCloser = true
		case ';', ',':
			// allowed
		default:
			return false
		}
	}
	return hasCloser
}

// ═══════════════════════════════════════════════════════════════════════════
// Block resolution
// ═══════════════════════════════════════════════════════════════════════════

func resolveBlockHunks(hunks []Hunk, lines []string, resolver BlockResolver, path string) ([]Hunk, error) {
	if resolver == nil {
		for _, h := range hunks {
			if h.Block {
				return nil, fmt.Errorf("block hunk at line %d requires a block resolver (unsupported file type?)", h.Start)
			}
		}
		return hunks, nil
	}

	var result []Hunk
	for _, h := range hunks {
		if !h.Block {
			result = append(result, h)
			continue
		}

		span, err := resolver(path, h.Start)
		if err != nil {
			return nil, fmt.Errorf("block resolution failed at line %d: %w", h.Start, err)
		}
		if span == nil {
			return nil, fmt.Errorf("no code block found at line %d", h.Start)
		}

		if span.Start < 1 || span.End > len(lines) || span.Start > span.End {
			return nil, fmt.Errorf("block at line %d resolved to invalid range %d..%d (file has %d lines)",
				h.Start, span.Start, span.End, len(lines))
		}

		switch h.Kind {
		case HunkReplace:
			result = append(result, Hunk{
				Kind:    HunkReplace,
				Start:   span.Start,
				End:     span.End,
				Payload: h.Payload,
			})
		case HunkDelete:
			result = append(result, Hunk{
				Kind:  HunkDelete,
				Start: span.Start,
				End:   span.End,
			})
		case HunkInsert:
			result = append(result, Hunk{
				Kind:    HunkInsert,
				Start:   span.End,
				Cursor:  "after",
				Payload: h.Payload,
			})
		}
	}
	return result, nil
}

// collapseExcessBlankLines reduces runs of three or more consecutive empty
// lines to exactly two. Single and double blank lines are left untouched.
func collapseExcessBlankLines(lines []string) []string {
	if len(lines) < 3 {
		return lines
	}
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun > 2 {
				continue
			}
		} else {
			blankRun = 0
		}
		out = append(out, line)
	}
	return out
}
