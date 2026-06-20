// diff.go — edit result formatting and diff preview orchestration.

package edit

import (
	"fmt"
	"strings"

	"nekocode/bot/tools/editdsl"
)

// formatEditResult returns the new tag + compact diff preview + full file
// line-number view so the agent can chain edits to any region without re-reading.
func formatEditResult(path string, oldText, newText string, hunks []editdsl.Hunk, newTag string, recovered bool, oldToNew map[int]int) string {
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

	preview := buildDiffPreview(oldText, newText, hunks, oldToNew)
	if preview == "" {
		sb.WriteString("(no changes)\n")
	} else {
		sb.WriteString(preview)
	}

	// Append full-file line-number view (like read output) so the agent can
	// see line numbers for any region, not just the diff context.
	newLines := strings.Split(strings.TrimRight(newText, "\n"), "\n")
	total := len(newLines)
	changedSet := buildChangedLineSet(hunks, oldText, newText, oldToNew)
	const elideThreshold = 20 // unchanged runs longer than this get elided
	const contextLines = 3    // context lines shown around each hunk
	shown := make(map[int]bool)
	for _, h := range hunks {
		lo := h.Start - contextLines
		if lo < 1 {
			lo = 1
		}
		hi := h.End + contextLines
		if h.Kind == editdsl.HunkInsert {
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
	return sb.String()
}

// buildDiffPreview generates a hunk-aware compact diff. Uses the parsed hunks
// to produce clean del/ins blocks instead of relying on whole-file LCS, which
// fragments when old and new text share scattered lines.
func buildDiffPreview(oldText, newText string, hunks []editdsl.Hunk, oldToNew map[int]int) string {
	oldText = strings.TrimRight(oldText, "\n")
	newText = strings.TrimRight(newText, "\n")
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	if oldText == newText && len(oldLines) == len(newLines) {
		return "(no changes)"
	}
	return formatHunkDiff(oldLines, newLines, hunks, oldToNew)
}
