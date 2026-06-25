package editcore

import (
	"fmt"
	"strings"
)

const mismatchContextLines = 2

// MismatchError is returned when a section's snapshot tag doesn't match the
// live file content and recovery is unavailable or has failed. It carries
// the file lines plus the anchor lines referenced by the patch hunks so
// callers can render a diagnostic with context.
type MismatchError struct {
	Path             string
	ExpectedFileHash string
	ActualFileHash   string
	FileLines        []string
	AnchorLines      []int
	// HashRecognized is true when the expected hash resolved to a recorded
	// snapshot (file content drifted since that snapshot), false when no
	// snapshot was ever recorded for the hash (likely fabricated or carried
	// over from a prior session).
	HashRecognized bool
}

func (e *MismatchError) Error() string {
	return e.formatMessage()
}

func (e *MismatchError) formatMessage() string {
	var sb strings.Builder
	for _, line := range mismatchRejectionHeader(e.Path, e.ExpectedFileHash, e.ActualFileHash, e.HashRecognized) {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	ctx := formatAnchoredContext(e.AnchorLines, e.FileLines)
	if len(ctx) > 0 {
		sb.WriteByte('\n')
		for _, line := range ctx {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// mismatchRejectionHeader returns the human-readable rejection message lines.
func mismatchRejectionHeader(path, expected, actual string, hashRecognized bool) []string {
	pathText := ""
	if path != "" {
		pathText = " for " + path
	}
	if !hashRecognized {
		return []string{
			fmt.Sprintf("Edit rejected%s: hash #%s is not from this session.", pathText, expected),
			fmt.Sprintf("The current file hashes to #%s. Re-read the file with read to copy a current [path#tag] header — never invent the tag and never reuse one from a prior session.", actual),
		}
	}
	return []string{
		fmt.Sprintf("Edit rejected%s: file changed between read and edit.", pathText),
		fmt.Sprintf("Section is bound to #%s, but the current file hashes to #%s.", expected, actual),
		"Re-read the file with read to refresh the tag before retrying.",
	}
}

// formatAnchoredContext produces numbered line rows around the given anchor
// lines, with * markers on the anchors. Returns nil if no anchors are in range.
func formatAnchoredContext(anchorLines []int, fileLines []string) []string {
	if len(anchorLines) == 0 || len(fileLines) == 0 {
		return nil
	}
	display := make(map[int]bool)
	for _, line := range anchorLines {
		if line < 1 || line > len(fileLines) {
			continue
		}
		lo := line - mismatchContextLines
		if lo < 1 {
			lo = 1
		}
		hi := line + mismatchContextLines
		if hi > len(fileLines) {
			hi = len(fileLines)
		}
		for l := lo; l <= hi; l++ {
			display[l] = true
		}
	}
	if len(display) == 0 {
		return nil
	}
	anchorSet := make(map[int]bool)
	for _, l := range anchorLines {
		anchorSet[l] = true
	}
	var rows []string
	prev := -1
	for line := 1; line <= len(fileLines); line++ {
		if !display[line] {
			continue
		}
		if prev != -1 && line > prev+1 {
			rows = append(rows, "...")
		}
		prev = line
		marker := " "
		if anchorSet[line] {
			marker = "*"
		}
		rows = append(rows, fmt.Sprintf("%s%d: %s", marker, line, fileLines[line-1]))
	}
	return rows
}

// CollectAnchorLines extracts the unique line numbers referenced by a list
// of hunks as anchors. Head/tail inserts contribute no anchor.
func CollectAnchorLines(hunks []Hunk) []int {
	seen := make(map[int]bool)
	var result []int
	for _, h := range hunks {
		if h.Kind == HunkInsert && (h.Cursor == CursorHead || h.Cursor == CursorTail) {
			continue
		}
		n := h.Start
		if n < 1 {
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		result = append(result, n)
	}
	return result
}
