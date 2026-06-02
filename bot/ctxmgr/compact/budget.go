package compact

import (
	"strings"

	"nekocode/bot/debug"
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
		itoa(len(lines)-grepHeadLines-grepTailLines) + " lines truncated] ...\n" +
		strings.Join(tail, "\n")
	debug.Log("budget_result: truncated grep from %d lines (%d chars) to head=%d tail=%d",
		len(lines), len(content), grepHeadLines, grepTailLines)
	return truncated, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
