package edit

import (
	"fmt"
	"strings"
)

func formatEditResult(path string, oldText, newText string, hunks []editHunk, newTag string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s#%s]\n", path, newTag)
	preview := buildDiffPreview(oldText, newText, hunks)
	if preview == "" {
		sb.WriteString("(no changes)\n")
	} else {
		sb.WriteString(preview)
	}

	newLines := splitDiffLines(newText)
	changedSet := buildChangedLineSet(hunks)
	if len(newLines) == 0 {
		return sb.String()
	}
	sb.WriteString("\n---\n")
	shown := shownLinesForHunks(hunks, len(newLines), 3)
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

func buildDiffPreview(oldText, newText string, hunks []editHunk) string {
	if oldText == newText {
		return "(no changes)"
	}
	oldLines := splitDiffLines(oldText)
	var sb strings.Builder
	shownOld := make(map[int]bool)
	var lastShown int
	for _, h := range hunks {
		ctxStart := max(1, h.OldStart-3)
		for line := ctxStart; line < h.OldStart && line <= len(oldLines); line++ {
			if shownOld[line] {
				continue
			}
			writeDiffGap(&sb, lastShown, line)
			fmt.Fprintf(&sb, " %d:%s\n", line, oldLines[line-1])
			shownOld[line] = true
			lastShown = line
		}
		for i, line := range h.OldLines {
			lineNo := h.OldStart + i
			fmt.Fprintf(&sb, "-%d:%s\n", lineNo, line)
			shownOld[lineNo] = true
			lastShown = lineNo
		}
		for i, line := range h.NewLines {
			fmt.Fprintf(&sb, "+%d:%s\n", h.NewStart+i, line)
		}
		ctxEnd := min(len(oldLines), h.OldEnd+3)
		for line := h.OldEnd + 1; line <= ctxEnd; line++ {
			if shownOld[line] {
				continue
			}
			writeDiffGap(&sb, lastShown, line)
			fmt.Fprintf(&sb, " %d:%s\n", line, oldLines[line-1])
			shownOld[line] = true
			lastShown = line
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func shownLinesForHunks(hunks []editHunk, total, context int) map[int]bool {
	shown := make(map[int]bool)
	for _, h := range hunks {
		start := max(1, h.NewStart-context)
		end := min(total, max(h.NewEnd, h.NewStart)+context)
		for line := start; line <= end; line++ {
			shown[line] = true
		}
	}
	return shown
}

func writeDiffGap(sb *strings.Builder, lastShown, next int) {
	if lastShown > 0 && next > lastShown+1 {
		gap := next - lastShown - 1
		if gap >= 8 {
			fmt.Fprintf(sb, " … (%d unchanged lines)\n", gap)
		}
	}
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
