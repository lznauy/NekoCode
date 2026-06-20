package runner

import (
	"fmt"
	"strings"
)

const (
	maxLines = 2000
	headLen  = 40
	tailLen  = 20
)

func truncateOutput(output string) string {
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}

	tailStart := max(len(lines)-tailLen, headLen)

	var b strings.Builder
	for i := range headLen {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	skipped := tailStart - headLen
	if skipped > 0 {
		fmt.Fprintf(&b, "\n[... %d lines truncated ...]\n\n", skipped)
	}
	for i := range len(lines) - tailStart {
		b.WriteString(lines[tailStart+i])
		b.WriteByte('\n')
	}
	return b.String()
}
