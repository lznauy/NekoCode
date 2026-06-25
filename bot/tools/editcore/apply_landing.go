package editcore

import (
	"fmt"
	"strings"
)

// RepairAfterInsertLandings slides shallow insert-after payloads past trailing
// structural closers when the chosen anchor sits inside a nested block.
func RepairAfterInsertLandings(sorted []Hunk, fileLines []string) ([]Hunk, []string) {
	if len(sorted) == 0 {
		return sorted, nil
	}

	targetedLines := make(map[int]bool)
	for _, h := range sorted {
		if h.Kind == HunkDelete {
			for l := h.Start; l <= h.End; l++ {
				targetedLines[l] = true
			}
		} else if h.Kind == HunkInsert && h.Cursor != CursorHead && h.Cursor != CursorTail {
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
		if h.Kind != HunkInsert || h.Cursor != CursorAfter || h.Start < 1 || h.Start > len(fileLines) {
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
			if targetedLines[line] {
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
				"insert after %d: body indented shallower than anchor line %q, "+
					"landing shifted past %d closing line(s) to after %d. "+
					"Your insert anchor sits inside a nested block — next time anchor on a line at (or shallower than) the body's target depth.",
				origAnchor, strings.TrimSpace(anchorText), crossed, landing))
		}
	}
	return sorted, warnings
}

func bodyTargetIndent(rows []string) (string, bool) {
	var target string
	first := true
	for _, row := range rows {
		trimmed := strings.TrimSpace(row)
		if trimmed == "" || isStructuralCloser(trimmed) {
			continue
		}
		indent := leadingIndent(row)
		if first {
			target = indent
			first = false
			continue
		}
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
