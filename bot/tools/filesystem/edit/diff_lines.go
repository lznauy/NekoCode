// diff_lines.go — changed-line mapping and full-file views for edit results.

package edit

import (
	"fmt"
	"strings"

	"nekocode/bot/tools/editcore"
)

// buildChangedLineSet returns the set of 1-based line numbers in the new file
// that were affected by the given hunks.
func buildChangedLineSet(hunks []editcore.Hunk, oldText, newText string, oldToNew map[int]int) map[int]bool {
	result := make(map[int]bool)
	if len(hunks) == 0 {
		return result
	}
	newLines := strings.Split(strings.TrimRight(newText, "\n"), "\n")

	for _, h := range hunks {
		switch h.Kind {
		case editcore.HunkReplace:
			for _, ni := range findNewLineRange(h, oldToNew, len(newLines)) {
				if ni >= 1 && ni <= len(newLines) {
					result[ni] = true
				}
			}
		case editcore.HunkDelete:
			// Deleted lines are gone, no new counterpart.
		case editcore.HunkInsert:
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
func findNewLineRange(h editcore.Hunk, oldToNew map[int]int, newLen int) []int {
	payloadLen := len(h.Payload)
	if payloadLen == 0 {
		return nil
	}
	switch h.Kind {
	case editcore.HunkInsert:
		start := computeInsertStart(h, oldToNew, newLen)
		result := make([]int, payloadLen)
		for i := 0; i < payloadLen; i++ {
			result[i] = start + i
		}
		return result
	case editcore.HunkReplace:
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
// that differ from oldText. It avoids hunk line numbers for recovery paths,
// where the hunk positions may reference a stale snapshot.
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
// changed-set markers to sb.
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
}

// computeInsertStart returns the 1-based line number where the first payload
// row of an insert hunk lands in the post-edit file.
func computeInsertStart(h editcore.Hunk, oldToNew map[int]int, newLen int) int {
	switch h.Cursor {
	case editcore.CursorHead:
		return 1
	case editcore.CursorTail:
		return newLen - len(h.Payload) + 1
	case editcore.CursorBefore:
		if ns, ok := oldToNew[h.Start]; ok && ns > 0 {
			return ns - len(h.Payload)
		}
		return h.Start
	case editcore.CursorAfter:
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
