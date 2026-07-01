package compact

import (
	"strconv"
	"strings"

	"nekocode/common/debug"
)

const grepHeadLines = 50
const grepTailLines = 50

// BudgetResult truncates a tool result before storage.
// For grep: keeps first 50 and last 50 lines, truncates middle.
// Returns the (possibly truncated) content and whether truncation occurred.
func BudgetResult(content string, toolName string) (string, bool) {
	if toolName != "grep" {
		return content, false
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= grepHeadLines+grepTailLines {
		return content, false
	}
	head := lines[:grepHeadLines]
	tail := lines[len(lines)-grepTailLines:]
	truncated := strings.Join(head, "\n") + "\n... [" +
		strconv.Itoa(len(lines)-grepHeadLines-grepTailLines) + " lines truncated] ...\n" +
		strings.Join(tail, "\n")
	debug.Log("budget_result: truncated grep from %d lines (%d chars) to head=%d tail=%d",
		len(lines), len(content), grepHeadLines, grepTailLines)
	return truncated, true
}
