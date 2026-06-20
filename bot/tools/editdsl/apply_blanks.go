package editdsl

import "strings"

// collapseExcessBlankLines reduces runs of three or more consecutive empty
// lines to exactly two and returns removed indices in the original slice.
func collapseExcessBlankLines(lines []string) ([]string, int, []int) {
	if len(lines) < 3 {
		return lines, 0, nil
	}
	out := make([]string, 0, len(lines))
	blankRun := 0
	collapsed := 0
	var removedIndices []int
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun > 2 {
				collapsed++
				removedIndices = append(removedIndices, i)
				continue
			}
		} else {
			blankRun = 0
		}
		out = append(out, line)
	}
	return out, collapsed, removedIndices
}
