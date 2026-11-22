package components

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"nekocode/interaction/tui/styles"
)

// The editor stores rune columns. Render and navigate soft lines in terminal
// cells so a wide character is moved intact when only one cell remains.
type inputRow struct {
	text  string
	start int // rune offset in the editable display text, including hard newlines
}
type inputLayout struct {
	rows               []inputRow
	row, column, first int
	line, runeColumn   int
}

func (i *Input) layout() inputLayout {
	line, col := i.textarea.Line(), i.textarea.Column()
	if cached := i.cachedLayout; cached != nil && cached.line == line && cached.runeColumn == col {
		return *cached
	}
	width := max(1, i.width-promptCols)
	cursor := i.cursorRuneOffset()
	layout := inputLayout{line: line, runeColumn: col}
	offset := 0
	for line := range strings.SplitSeq(i.textarea.Value(), "\n") {
		row := line
		cells, rowStart := 0, offset
		for rest := line; rest != ""; {
			cluster, size := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
			if cells > 0 && cells+size > width {
				layout.rows = append(layout.rows, inputRow{row[:len(row)-len(rest)], rowStart})
				row, rowStart, cells = rest, offset, 0
			}
			cells += size
			offset += utf8.RuneCountInString(cluster)
			rest = rest[len(cluster):]
		}
		layout.rows = append(layout.rows, inputRow{row, rowStart})
		if cells >= width {
			layout.rows = append(layout.rows, inputRow{"", offset})
		}
		offset++ // hard newline
	}
	for n, row := range layout.rows {
		if row.start > cursor {
			break
		}
		layout.row = n
	}
	current := layout.rows[layout.row]
	layout.column = ansi.StringWidth(string([]rune(current.text)[:cursor-current.start]))
	layout.first = max(0, layout.row-maxInputLines+1)
	i.cachedLayout = &layout
	return layout
}

func (i *Input) CanCursorUp() bool { return i.layout().row > 0 }
func (i *Input) CanCursorDown() bool {
	l := i.layout()
	return l.row+1 < len(l.rows)
}

func (i *Input) moveVisualRow(step int) {
	l := i.layout()
	target := l.row + step
	if target < 0 || target >= len(l.rows) {
		return
	}
	if i.preferredColumn < 0 {
		i.preferredColumn = l.column
	}
	row := l.rows[target]
	offset := row.start
	column := 0
	for text := row.text; text != ""; {
		cluster, width := ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
		if column+width > i.preferredColumn {
			break
		}
		next := offset + utf8.RuneCountInString(cluster)
		if target+1 < len(l.rows) && next == l.rows[target+1].start {
			break
		}
		offset, column, text = next, column+width, text[len(cluster):]
	}
	i.setCursorRuneOffset(offset)
}

func (i *Input) Height() int { return 4 + min(maxInputLines, len(i.layout().rows)) }

func (i *Input) Cursor() *tea.Cursor {
	l := i.layout()
	c := tea.NewCursor(promptCols+l.column, 1+l.row-l.first)
	c.Shape = tea.CursorBar
	return c
}

func (i *Input) TextView() string {
	if !i.HasContent() {
		return strings.TrimRight(i.textarea.View(), "\n")
	}
	l := i.layout()
	prompt := styles.CatEyeStyle.Bold(true).Render("┃ ")
	if i.sending {
		prompt = styles.MutedStyle.Render("⋯ ")
	}
	lines := make([]string, 0, maxInputLines)
	for _, row := range l.rows[l.first:min(len(l.rows), l.first+maxInputLines)] {
		lines = append(lines, prompt+row.text)
	}
	return strings.Join(lines, "\n")
}

// Use terminal grapheme boundaries for character edits. Handling deletions
// here also keeps a forward delete at the last character from joining lines.
func (i *Input) editGrapheme(backward, deleting bool) bool {
	text := i.textarea.Value()
	cursor := i.cursorRuneOffset()
	probe := cursor
	if backward {
		probe--
	}
	if probe < 0 {
		return false
	}
	start := 0
	for rest := text; rest != ""; {
		cluster, _ := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		end := start + utf8.RuneCountInString(cluster)
		if probe < end {
			if !deleting && end-start == 1 {
				return false
			}
			if deleting {
				runes := []rune(text)
				updated := string(runes[:start]) + string(runes[end:])
				if i.reconcileDisplayEdit(text, updated, cursor) {
					i.cachedLayout = nil
					i.textarea.SetValue(updated)
					i.setCursorRuneOffset(start)
					i.syncDisplayCharLimit()
				}
			} else if backward {
				i.setCursorRuneOffset(start)
			} else {
				i.setCursorRuneOffset(end)
			}
			return true
		}
		start, rest = end, rest[len(cluster):]
	}
	return false
}
