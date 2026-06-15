// diff.go — diff formatting and line-mapping logic for the edit tool.
// Extracted from tool_edit.go to keep that file focused on DSL parsing and execution.

package builtin

import (
	"fmt"
	"sort"
	"strings"

	"nekocode/bot/tools/hashline"
)

// ---------------------------------------------------------------------------
// Hunk sorting (shared by buildOldToNewMapping and formatHunkDiff)
// ---------------------------------------------------------------------------

// hunkSortLess is the comparison function for sorting hunks by position.
// Head-cursor inserts sort first, tail-cursor inserts sort last.
func hunkSortLess(a, b hashline.Hunk) bool {
	if a.Kind == hashline.HunkInsert && a.Cursor == "head" {
		return true
	}
	if b.Kind == hashline.HunkInsert && b.Cursor == "head" {
		return false
	}
	if a.Kind == hashline.HunkInsert && a.Cursor == "tail" {
		return false
	}
	if b.Kind == hashline.HunkInsert && b.Cursor == "tail" {
		return true
	}
	return a.Start < b.Start
}

// sortedHunksAsc returns a copy of hunks sorted by ascending position.
func sortedHunksAsc(hunks []hashline.Hunk) []hashline.Hunk {
	sorted := make([]hashline.Hunk, len(hunks))
	copy(sorted, hunks)
	sort.Slice(sorted, func(i, j int) bool {
		return hunkSortLess(sorted[i], sorted[j])
	})
	return sorted
}

// sortedHunksDesc returns a copy of hunks sorted by descending position.
func sortedHunksDesc(hunks []hashline.Hunk) []hashline.Hunk {
	sorted := make([]hashline.Hunk, len(hunks))
	copy(sorted, hunks)
	sort.Slice(sorted, func(i, j int) bool {
		return hunkSortLess(sorted[j], sorted[i])
	})
	return sorted
}

// ---------------------------------------------------------------------------
// Edit result formatting
// ---------------------------------------------------------------------------

// formatEditResult returns the new tag + compact diff preview + full file
// line-number view so the agent can chain edits to any region without re-reading.
func formatEditResult(path string, oldText, newText string, hunks []hashline.Hunk, newTag string, recovered bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s#%s]\n", path, newTag)

	if recovered {
		// Recovery: hunks have snapshot line numbers that may not match the
		// current file. Use simple line-by-line comparison to avoid panics.
		changedSet := buildSimpleChangedSet(oldText, newText)
		sb.WriteString("(recovered via 3-way merge)\n")
		writeFullFileView(&sb, newText, changedSet, path)
		return sb.String()
	}

	preview := buildDiffPreview(oldText, newText, hunks)
	if preview == "" {
		sb.WriteString("(no changes)\n")
	} else {
		sb.WriteString(preview)
	}

	// Append full-file line-number view (like read output) so the agent can
	// see line numbers for any region, not just the diff context.
	newLines := strings.Split(strings.TrimRight(newText, "\n"), "\n")
	total := len(newLines)
	changedSet := buildChangedLineSet(hunks, oldText, newText)
	const elideThreshold = 20 // unchanged runs longer than this get elided
	const contextLines = 3    // context lines shown around each hunk
	shown := make(map[int]bool)
	for _, h := range hunks {
		lo := h.Start - contextLines
		if lo < 1 {
			lo = 1
		}
		hi := h.End + contextLines
		if h.Kind == hashline.HunkInsert {
			// show context around the insertion anchor
			lo = h.Start - contextLines
			if lo < 1 {
				lo = 1
			}
			hi = h.Start + contextLines + len(h.Payload)
		}
		if hi > total {
			hi = total
		}
		for l := lo; l <= hi; l++ {
			shown[l] = true
		}
		// Mark changed lines in the shown set.
		for cl := range changedSet {
			shown[cl] = true
		}
	}
	// If no hunks (shouldn't happen for a real edit, but be defensive), show all.
	if len(hunks) == 0 {
		for i := 1; i <= total; i++ {
			shown[i] = true
		}
	}

	sb.WriteString("\n---\n")
	var lastShown int
	for line := 1; line <= total; line++ {
		if !shown[line] {
			continue
		}
		if lastShown > 0 && line > lastShown+1 {
			gap := line - lastShown - 1
			if gap > elideThreshold {
				fmt.Fprintf(&sb, "… (%d unchanged lines)\n", gap)
			} else {
				for l := lastShown + 1; l < line; l++ {
					fmt.Fprintf(&sb, " %d:%s\n", l, newLines[l-1])
				}
			}
		} else if lastShown == 0 && line > 1 {
			// Leading gap: lines 1..line-1 are not shown.
			fmt.Fprintf(&sb, "… (%d lines)\n", line-1)
		}
		prefix := " "
		if changedSet[line] {
			prefix = "*"
		}
		fmt.Fprintf(&sb, "%s%d:%s\n", prefix, line, newLines[line-1])
		lastShown = line
	}
	// Trailing gap: lines after lastShown.
	if lastShown > 0 && lastShown < total {
		fmt.Fprintf(&sb, "… (%d lines)\n", total-lastShown)
	}
	fmt.Fprintf(&sb, "(total=%d lines)\n", total)
	fmt.Fprintf(&sb, "Undo mistake: call edit with revert=true, patch=%q\n", path)
	return sb.String()
}

// buildChangedLineSet returns the set of 1-based line numbers in the new file
// that were affected by the given hunks.
func buildChangedLineSet(hunks []hashline.Hunk, oldText, newText string) map[int]bool {
	result := make(map[int]bool)
	if len(hunks) == 0 {
		return result
	}
	oldLines := strings.Split(strings.TrimRight(oldText, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(newText, "\n"), "\n")

	// Simulate the apply to produce old->new mapping.
	oldToNew := buildOldToNewMapping(hunks, len(oldLines), oldLines)

	for _, h := range hunks {
		switch h.Kind {
		case hashline.HunkReplace:
			for _, ni := range findNewLineRange(h, oldToNew, len(newLines)) {
				if ni >= 1 && ni <= len(newLines) {
					result[ni] = true
				}
			}
		case hashline.HunkDelete:
			// Deleted lines are gone, no new counterpart.
		case hashline.HunkInsert:
			for _, ni := range findNewLineRange(h, oldToNew, len(newLines)) {
				if ni >= 1 && ni <= len(newLines) {
					result[ni] = true
				}
			}
		}
	}
	return result
}

// findNewLineRange returns the 1-based line numbers in the new file that
// correspond to a hunk's added or replaced payload lines.
func findNewLineRange(h hashline.Hunk, oldToNew map[int]int, newLen int) []int {
	payloadLen := len(h.Payload)
	if payloadLen == 0 {
		return nil
	}
	switch h.Kind {
	case hashline.HunkInsert:
		start := computeInsertStart(h, oldToNew, newLen)
		result := make([]int, payloadLen)
		for i := 0; i < payloadLen; i++ {
			result[i] = start + i
		}
		return result
	case hashline.HunkReplace:
		// Find where the old h.Start maps to in the new file.
		start, ok := oldToNew[h.Start]
		if !ok || start < 1 {
			return nil
		}
		result := make([]int, payloadLen)
		for i := 0; i < payloadLen; i++ {
			result[i] = start + i
		}
		return result
	default:
		return nil
	}
}

// buildSimpleChangedSet returns the set of 1-based new-file line numbers
// that differ from oldText. Uses simple line-by-line comparison without hunk
// line numbers, so it is safe to call during recovery (where hunk line numbers
// reference a stale snapshot).
func buildSimpleChangedSet(oldText, newText string) map[int]bool {
	oldText = strings.TrimRight(oldText, "\n")
	newText = strings.TrimRight(newText, "\n")
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	result := make(map[int]bool)
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}
	for i := 0; i < maxLen; i++ {
		var o, n string
		if i < len(oldLines) {
			o = oldLines[i]
		}
		if i < len(newLines) {
			n = newLines[i]
		}
		if o != n {
			result[i+1] = true
		}
	}
	return result
}

// writeFullFileView writes the complete new file with line numbers and
// changed-set markers to sb. Used during recovery to give the agent full
// line-number context without relying on stale hunk ranges.
func writeFullFileView(sb *strings.Builder, newText string, changedSet map[int]bool, path string) {
	newLines := strings.Split(strings.TrimRight(newText, "\n"), "\n")
	sb.WriteString("\n---\n")
	for i, line := range newLines {
		prefix := " "
		if changedSet[i+1] {
			prefix = "*"
		}
		fmt.Fprintf(sb, "%s%d:%s\n", prefix, i+1, line)
	}
	fmt.Fprintf(sb, "Undo mistake: call edit with revert=true, patch=%q\n", path)
}

// buildDiffPreview generates a hunk-aware compact diff. Uses the parsed hunks
// to produce clean del/ins blocks instead of relying on whole-file LCS, which
// fragments when old and new text share scattered lines.
func buildDiffPreview(oldText, newText string, hunks []hashline.Hunk) string {
	oldText = strings.TrimRight(oldText, "\n")
	newText = strings.TrimRight(newText, "\n")
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	if oldText == newText && len(oldLines) == len(newLines) {
		return "(no changes)"
	}
	return formatHunkDiff(oldLines, newLines, hunks)
}

// buildOldToNewMapping simulates the edit apply to produce a
// deterministic old-line → new-line mapping. Uses the same bottom-up
// hunk order and after-insert landing shifts as ApplyEdits so context
// line numbers in the diff preview are always correct.
func buildOldToNewMapping(hunks []hashline.Hunk, oldLen int, oldLines []string) map[int]int {
	type tracked struct{ orig int } // 0 = inserted
	lines := make([]tracked, oldLen)
	for i := range lines {
		lines[i] = tracked{orig: i + 1}
	}
	sorted := sortedHunksDesc(hunks)
	sorted, _ = hashline.RepairAfterInsertLandings(sorted, oldLines)
	for _, h := range sorted {
		switch h.Kind {
		case hashline.HunkReplace:
			start := h.Start - 1
			if start < 0 {
				start = 0
			}
			end := h.End
			if end > len(lines) {
				end = len(lines)
			}
			ins := make([]tracked, len(h.Payload))
			lines = append(append(lines[:start], ins...), lines[end:]...)
		case hashline.HunkDelete:
			start := h.Start - 1
			if start < 0 {
				start = 0
			}
			end := h.End
			if end > len(lines) {
				end = len(lines)
			}
			lines = append(lines[:start], lines[end:]...)
		case hashline.HunkInsert:
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
			ins := make([]tracked, len(h.Payload))
			lines = append(append(lines[:idx], ins...), lines[idx:]...)
		}
	}
	result := make(map[int]int)
	for newIdx, l := range lines {
		if l.orig > 0 {
			result[l.orig] = newIdx + 1
		}
	}
	return result
}

func formatHunkDiff(oldLines, newLines []string, hunks []hashline.Hunk) string {
	type outLine struct {
		prefix string
		text   string
	}

	// Build old->new mapping by simulating the actual apply.
	oldToNew := buildOldToNewMapping(hunks, len(oldLines), oldLines)

	sorted := sortedHunksAsc(hunks)

	var out []outLine
	const contextLines = 3
	shown := make(map[int]bool)
	lastShown := 0

	for _, h := range sorted {
		// ---- context before hunk ----
		ctxStart := h.Start - contextLines
		if ctxStart < 1 {
			ctxStart = 1
		}
		ctxBeforeEnd := h.Start
		if h.Kind == hashline.HunkInsert && h.Cursor == "after" {
			ctxBeforeEnd = h.Start + 1 // include anchor line in context-before
		}
		for i := ctxStart; i < ctxBeforeEnd; i++ {
			if shown[i] {
				continue
			}
			if lastShown > 0 && i > lastShown+1 {
				gap := i - lastShown - 1
				if gap >= 8 {
					out = append(out, outLine{text: fmt.Sprintf("… (%d unchanged lines)", gap)})
				}
			}
			newNo, ok := oldToNew[i]
			if ok && newNo > 0 {
				out = append(out, outLine{prefix: fmt.Sprintf(" %d:", newNo), text: oldLines[i-1]})
			} else {
				out = append(out, outLine{prefix: fmt.Sprintf(" %d:", i), text: oldLines[i-1]})
			}
			shown[i] = true
			lastShown = i
		}

		// ---- hunk body ----
		switch h.Kind {
		case hashline.HunkReplace:
			for l := h.Start; l <= h.End; l++ {
				out = append(out, outLine{prefix: fmt.Sprintf("-%d:", l), text: safeGet(oldLines, l-1)})
				shown[l] = true
				lastShown = l
			}
			// Compute new line number for inserted content.
			insStart := h.Start // fallback: assume same position
			if ns, ok := oldToNew[h.Start]; ok && ns > 0 {
				insStart = ns
			} else if h.Start > 1 {
				if ns, ok := oldToNew[h.Start-1]; ok && ns > 0 {
					insStart = ns + 1
				}
			}
			for k, line := range h.Payload {
				out = append(out, outLine{prefix: fmt.Sprintf("+%d:", insStart+k), text: line})
			}

		case hashline.HunkDelete:
			for l := h.Start; l <= h.End; l++ {
				out = append(out, outLine{prefix: fmt.Sprintf("-%d:", l), text: safeGet(oldLines, l-1)})
				shown[l] = true
				lastShown = l
			}

		case hashline.HunkInsert:
			insStart := computeInsertStart(h, oldToNew, len(newLines))
			for k, line := range h.Payload {
				out = append(out, outLine{prefix: fmt.Sprintf("+%d:", insStart+k), text: line})
			}
		}

		// ---- context after hunk ----
		hunkEnd := h.End
		if h.Kind == hashline.HunkInsert {
			hunkEnd = h.Start
			if h.Cursor == "after" {
				hunkEnd = h.Start + 1 // anchor stays before insert; skip it in context-after
			}
		}
		if hunkEnd < 1 {
			hunkEnd = 1
		}
		ctxEnd := hunkEnd + contextLines
		if ctxEnd > len(oldLines) {
			ctxEnd = len(oldLines)
		}
		for i := hunkEnd; i <= ctxEnd; i++ {
			if shown[i] {
				continue
			}
			if lastShown > 0 && i > lastShown+1 {
				gap := i - lastShown - 1
				if gap >= 8 {
					out = append(out, outLine{text: fmt.Sprintf("… (%d unchanged lines)", gap)})
				}
			}
			newNo, ok := oldToNew[i]
			if ok && newNo > 0 {
				out = append(out, outLine{prefix: fmt.Sprintf(" %d:", newNo), text: oldLines[i-1]})
			} else {
				out = append(out, outLine{prefix: fmt.Sprintf(" %d:", i), text: oldLines[i-1]})
			}
			shown[i] = true
			lastShown = i
		}
	}

	var sb strings.Builder
	for _, l := range out {
		if l.prefix == "" {
			fmt.Fprintf(&sb, " %s\n", l.text)
		} else {
			fmt.Fprintf(&sb, "%s%s\n", l.prefix, l.text)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// computeInsertStart returns the 1-based line number where the first
// payload row of an insert hunk lands in the post-edit file.
func computeInsertStart(h hashline.Hunk, oldToNew map[int]int, newLen int) int {
	switch h.Cursor {
	case "head":
		return 1
	case "tail":
		return newLen - len(h.Payload) + 1
	case "before":
		// Anchor content shifted down by len(Payload); insert lands before it.
		if ns, ok := oldToNew[h.Start]; ok && ns > 0 {
			return ns - len(h.Payload)
		}
		return h.Start
	case "after":
		if ns, ok := oldToNew[h.Start]; ok && ns > 0 {
			return ns + 1
		}
		return h.Start + 1
	default:
		return h.Start
	}
}

func safeGet(lines []string, idx int) string {
	if idx < 0 || idx >= len(lines) {
		return "<EOF>"
	}
	return lines[idx]
}
