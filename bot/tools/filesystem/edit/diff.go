package edit

import (
	"fmt"
	"strings"

	"nekocode/bot/tools/diff"
)

var editTextChangeOptions = diff.TextChangeOptions{
	Context:      diff.DefaultContext,
	NoChangeText: diff.NoChanges,
}

// formatEditResult renders edit result with file header + diff preview + changed lines.
func formatEditResult(path string, oldText, newText string, hunks []editHunk, newTag string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", diff.TagHeader(path, newTag))
	sb.WriteString(diff.RenderTextChange(oldText, newText, editTextChangeOptions))

	newLines := diff.SplitLines(newText)
	if len(newLines) == 0 {
		return sb.String()
	}
	sb.WriteString("\n---\n")
	shown := shownLinesForHunks(hunks, len(newLines), 3)
	changedSet := buildChangedLineSet(hunks)
	var lastShown int
	for line := 1; line <= len(newLines); line++ {
		if !shown[line] {
			continue
		}
		writeGap(&sb, lastShown, line)
		prefix := " "
		if changedSet[line] {
			prefix = "*"
		}
		fmt.Fprintf(&sb, "%s%d:%s\n", prefix, line, newLines[line-1])
		lastShown = line
	}
	if lastShown > 0 && lastShown < len(newLines) {
		fmt.Fprintf(&sb, "… (%d lines)\n", len(newLines)-lastShown)
	}
	return sb.String()
}

// shownLinesForHunks determines which lines to display given hunk positions.
func shownLinesForHunks(hunks []editHunk, total, context int) map[int]bool {
	shown := make(map[int]bool)
	for _, h := range hunks {
		start := maxInt(1, h.NewStart-context)
		end := minInt(total, maxInt(h.NewEnd, h.NewStart)+context)
		for line := start; line <= end; line++ {
			shown[line] = true
		}
	}
	return shown
}

// buildChangedLineSet returns the set of new line numbers that were changed.
func buildChangedLineSet(hunks []editHunk) map[int]bool {
	result := make(map[int]bool)
	for _, h := range hunks {
		if len(h.NewLines) == 0 {
			continue
		}
		for line := h.NewStart; line <= h.NewEnd; line++ {
			if line > 0 {
				result[line] = true
			}
		}
	}
	return result
}

func writeGap(sb *strings.Builder, lastShown, next int) {
	if lastShown == 0 {
		if next > 1 {
			fmt.Fprintf(sb, "… (%d lines)\n", next-1)
		}
		return
	}
	if next > lastShown+1 {
		gap := next - lastShown - 1
		if gap > 20 {
			fmt.Fprintf(sb, "… (%d unchanged lines)\n", gap)
		}
	}
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
