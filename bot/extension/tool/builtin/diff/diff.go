// Package diff provides unified diff computation and rendering for the tool
// system. It is used both by the standalone "diff" tool (for arbitrary
// before/after comparison) and by the "edit" tool (for preview rendering).
package diff

import (
	"fmt"
	"strings"
)

const (
	DefaultContext    = 3
	NoChanges         = "(no changes)"
	maxExactDiffCells = 1_000_000
)

// LineKind classifies a line in a unified diff.
type LineKind string

const (
	LineCtx LineKind = "ctx"
	LineAdd LineKind = "add"
	LineDel LineKind = "del"
)

// DiffLine is one line of a unified diff output.
type DiffLine struct {
	Kind      LineKind
	OldNo     int // 0 for added lines
	NewNo     int // 0 for deleted lines
	Text      string
	NoNewline bool // This source line was not terminated by a newline.
}

// Hunk is a contiguous block of changes.
type Hunk struct {
	OldStart int
	OldEnd   int
	NewStart int
	NewEnd   int
	Lines    []DiffLine
}

// TextChangeOptions controls how a before/after text change is rendered.
type TextChangeOptions struct {
	Context      int
	Header       string
	NoChangeText string
}

// TagHeader renders the standard "[path#tag]" preview header.
func TagHeader(path, tag string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("[%s#%s]", path, tag)
}

// ToolHeader renders the standard "[tool path]" preview header.
func ToolHeader(tool, path string) string {
	if tool == "" || path == "" {
		return ""
	}
	return fmt.Sprintf("[%s %s]", tool, path)
}

// RenderTextChange renders a complete before/after preview. It is the shared
// entry point for tools that modify text and want the standard diff format.
func RenderTextChange(oldText, newText string, opts TextChangeOptions) string {
	hunks := ComputeDiff(oldText, newText, opts.Context)
	if len(hunks) == 0 {
		return opts.NoChangeText
	}

	body := strings.TrimRight(RenderHunks(hunks), "\n")
	if opts.Header == "" {
		return body
	}
	return strings.TrimRight(opts.Header, "\n") + "\n" + body
}

// ComputeDiff produces a line diff between old and new text.
func ComputeDiff(oldText, newText string, context int) []Hunk {
	if context < 0 {
		context = 0
	}
	oldLines := splitDiffLines(oldText)
	newLines := splitDiffLines(newText)
	// More context than the complete inputs adds nothing and can overflow
	// hunk offset arithmetic for otherwise valid large integer arguments.
	context = min(context, max(len(oldLines), len(newLines)))
	if len(oldLines) == 0 && len(newLines) == 0 {
		return nil
	}
	if len(oldLines)*len(newLines) > maxExactDiffCells {
		return computeGreedyDiff(oldLines, newLines, context)
	}
	return buildHunks(lcsOps(oldLines, newLines), context)
}

func lcsOps(oldLines, newLines []string) []DiffLine {
	m, n := len(oldLines), len(newLines)
	dp := make([]int, (m+1)*(n+1))
	at := func(i, j int) int { return i*(n+1) + j }

	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[at(i, j)] = dp[at(i+1, j+1)] + 1
			} else if dp[at(i+1, j)] >= dp[at(i, j+1)] {
				dp[at(i, j)] = dp[at(i+1, j)]
			} else {
				dp[at(i, j)] = dp[at(i, j+1)]
			}
		}
	}

	ops := make([]DiffLine, 0, m+n)
	i, j := 0, 0
	for i < m || j < n {
		if i < m && j < n && oldLines[i] == newLines[j] {
			ops = append(ops, makeDiffLine(LineCtx, i+1, j+1, oldLines[i]))
			i++
			j++
		} else if i < m && (j >= n || dp[at(i+1, j)] >= dp[at(i, j+1)]) {
			ops = append(ops, makeDiffLine(LineDel, i+1, 0, oldLines[i]))
			i++
		} else {
			ops = append(ops, makeDiffLine(LineAdd, 0, j+1, newLines[j]))
			j++
		}
	}
	return ops
}

func buildHunks(ops []DiffLine, context int) []Hunk {
	var hunks []Hunk
	for i := 0; i < len(ops); {
		for i < len(ops) && ops[i].Kind == LineCtx {
			i++
		}
		if i >= len(ops) {
			break
		}

		start := maxInt(0, i-context)
		lastChange := i
		trailingContext := 0
		j := i + 1
		for ; j < len(ops); j++ {
			if ops[j].Kind == LineCtx {
				trailingContext++
				if trailingContext > context {
					break
				}
				continue
			}
			trailingContext = 0
			lastChange = j
		}
		end := minInt(len(ops), lastChange+context+1)
		hunk := Hunk{Lines: append([]DiffLine(nil), ops[start:end]...)}
		finalizeHunk(&hunk)
		hunks = append(hunks, hunk)
		i = end
	}
	return hunks
}

func computeGreedyDiff(oldLines, newLines []string, context int) []Hunk {
	// Simple greedy diff: walk both inputs, emit adds/dels/context.
	var hunks []Hunk
	var cur *Hunk

	oi, ni := 0, 0
	for oi < len(oldLines) || ni < len(newLines) {
		if oi < len(oldLines) && ni < len(newLines) && oldLines[oi] == newLines[ni] {
			// Context line
			if cur != nil {
				cur.Lines = append(cur.Lines, makeDiffLine(LineCtx, oi+1, ni+1, oldLines[oi]))
				if len(cur.Lines) > context*2+1 {
					// Close hunk if too much context
					finalizeHunk(cur)
					hunks = append(hunks, *cur)
					cur = nil
				}
			}
			oi++
			ni++
			continue
		}

		// Start new hunk if needed
		if cur == nil {
			cur = &Hunk{
				OldStart: maxInt(1, oi+1-context),
				NewStart: maxInt(1, ni+1-context),
			}
			// Prepend context lines
			for i := cur.OldStart - 1; i < oi; i++ {
				cur.Lines = append(cur.Lines, makeDiffLine(LineCtx, i+1, ni+(i-(cur.OldStart-1))+1, oldLines[i]))
			}
		}

		// Decide: delete from old or add from new
		if oi < len(oldLines) && (ni >= len(newLines) || !containsAt(newLines, ni, oldLines, oi)) {
			cur.Lines = append(cur.Lines, makeDiffLine(LineDel, oi+1, 0, oldLines[oi]))
			oi++
		} else if ni < len(newLines) {
			cur.Lines = append(cur.Lines, makeDiffLine(LineAdd, 0, ni+1, newLines[ni]))
			ni++
		}
	}

	if cur != nil {
		finalizeHunk(cur)
		hunks = append(hunks, *cur)
	}
	return hunks
}

func finalizeHunk(h *Hunk) {
	oldMin, oldMax := 0, 0
	newMin, newMax := 0, 0
	for _, l := range h.Lines {
		if l.OldNo > 0 {
			if oldMin == 0 || l.OldNo < oldMin {
				oldMin = l.OldNo
			}
			if l.OldNo > oldMax {
				oldMax = l.OldNo
			}
		}
		if l.NewNo > 0 {
			if newMin == 0 || l.NewNo < newMin {
				newMin = l.NewNo
			}
			if l.NewNo > newMax {
				newMax = l.NewNo
			}
		}
	}
	h.OldStart = oldMin
	h.OldEnd = oldMax
	h.NewStart = newMin
	h.NewEnd = newMax
}

func containsAt(newLines []string, ni int, oldLines []string, oi int) bool {
	// Heuristic: does oldLines[oi] appear in newLines within next 5 positions?
	for i := ni; i < minInt(len(newLines), ni+5); i++ {
		if newLines[i] == oldLines[oi] {
			return true
		}
	}
	return false
}

// RenderHunks formats hunks as +NNN:text, -NNN:text, and NNN:text lines so
// edit, write, diff, TUI, and GUI can share one preview format.
func RenderHunks(hunks []Hunk) string {
	var sb strings.Builder
	for i, h := range hunks {
		if i > 0 {
			sb.WriteString("\n")
		}
		for _, l := range h.Lines {
			switch l.Kind {
			case LineAdd:
				fmt.Fprintf(&sb, "+%d:%s\n", l.NewNo, l.Text)
			case LineDel:
				fmt.Fprintf(&sb, "-%d:%s\n", l.OldNo, l.Text)
			default:
				fmt.Fprintf(&sb, " %d:%s\n", l.OldNo, l.Text)
			}
			if l.NoNewline {
				sb.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	return sb.String()
}

// Compare newline terminators as part of each line, without changing SplitLines
// semantics used by content-anchored editing. Render the terminator separately.
func splitDiffLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func makeDiffLine(kind LineKind, oldNo, newNo int, text string) DiffLine {
	return DiffLine{Kind: kind, OldNo: oldNo, NewNo: newNo, Text: strings.TrimSuffix(text, "\n"), NoNewline: !strings.HasSuffix(text, "\n")}
}

// SplitLines splits text into lines, dropping the trailing empty element from final newline.
func SplitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Drop trailing empty element from final newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
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
