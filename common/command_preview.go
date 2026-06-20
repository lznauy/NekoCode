package common

import (
	"fmt"
	"strings"
)

// FormatCommandPreview returns a compact display-only preview for shell commands.
// The returned value is never used for execution; callers must keep the original command.
func FormatCommandPreview(command string, maxRunes int) string {
	command = strings.TrimSpace(strings.ReplaceAll(command, "\r\n", "\n"))
	if command == "" || maxRunes <= 0 {
		return ""
	}
	lines := strings.Split(command, "\n")
	if len(lines) > 1 {
		return formatMultilineCommandPreview(lines, maxRunes, len([]rune(command)))
	}
	return truncateCommandMiddle(command, maxRunes)
}

func formatMultilineCommandPreview(lines []string, maxRunes, totalRunes int) string {
	first := firstNonEmptyLine(lines)
	last := lastNonEmptyLine(lines)
	extraLines := len(lines) - 1
	suffix := fmt.Sprintf(" (+%d lines, %d chars)", extraLines, totalRunes)
	if first == "" {
		return truncateCommandMiddle(strings.TrimSpace(suffix), maxRunes)
	}
	if last == "" || last == first {
		return truncateCommandMiddle(first+" ..."+suffix, maxRunes)
	}
	return truncateCommandMiddle(first+" ... "+last+suffix, maxRunes)
}

func firstNonEmptyLine(lines []string) string {
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func lastNonEmptyLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateCommandMiddle(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return "…"
	}
	if maxRunes <= 8 {
		return string(runes[:maxRunes-1]) + "…"
	}
	headLen := maxRunes * 2 / 3
	tailLen := maxRunes - headLen - 1
	return strings.TrimRight(string(runes[:headLen]), " ") + "…" + strings.TrimLeft(string(runes[len(runes)-tailLen:]), " ")
}
