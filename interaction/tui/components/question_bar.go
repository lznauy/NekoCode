package components

import (
	"fmt"
	"strings"

	"nekocode/interaction/tui/styles"
	controlruntime "nekocode/runtime"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type QuestionBar struct {
	req       *controlruntime.QuestionRequest
	sty       *styles.Styles
	activeQ   int
	activeOpt int
	selected  map[int]map[int]bool
	custom    []string
	customPos []int
	respond   func(controlruntime.QuestionReply)
}

func NewQuestionBar(sty *styles.Styles) *QuestionBar {
	return &QuestionBar{sty: sty, selected: make(map[int]map[int]bool)}
}

func (q *QuestionBar) SetRequest(req *controlruntime.QuestionRequest, respond func(controlruntime.QuestionReply)) {
	q.req = req
	q.activeQ = 0
	q.activeOpt = 0
	q.selected = make(map[int]map[int]bool)
	q.custom = make([]string, len(req.Questions))
	q.customPos = make([]int, len(req.Questions))
	q.respond = respond
}

func (q *QuestionBar) Clear() {
	q.req = nil
	q.respond = nil
}

func (q *QuestionBar) Move(delta int) {
	if q.req == nil || len(q.req.Questions) == 0 {
		return
	}
	item := q.req.Questions[q.activeQ]
	n := len(item.Options)
	if item.Custom {
		n++
	}
	if n == 0 {
		return
	}
	q.activeOpt = (q.activeOpt + delta + n) % n
}

func (q *QuestionBar) Toggle() {
	if q.req == nil || len(q.req.Questions) == 0 {
		return
	}
	item := q.req.Questions[q.activeQ]
	n := len(item.Options)
	if item.Custom {
		n++
	}
	if n == 0 {
		return
	}
	if !item.Multiple {
		q.selected[q.activeQ] = map[int]bool{q.activeOpt: true}
		return
	}
	if q.selected[q.activeQ] == nil {
		q.selected[q.activeQ] = make(map[int]bool)
	}
	q.selected[q.activeQ][q.activeOpt] = !q.selected[q.activeQ][q.activeOpt]
}

func (q *QuestionBar) Type(text string) {
	if q.req == nil || len(q.req.Questions) == 0 {
		return
	}
	item := q.req.Questions[q.activeQ]
	if !item.Custom || q.activeOpt != len(item.Options) {
		return
	}
	rs := []rune(q.custom[q.activeQ])
	pos := min(q.customPos[q.activeQ], len(rs))
	insert := []rune(text)
	q.custom[q.activeQ] = string(rs[:pos]) + string(insert) + string(rs[pos:])
	q.customPos[q.activeQ] = pos + len(insert)
}

func (q *QuestionBar) Backspace() {
	if q.req == nil || len(q.req.Questions) == 0 {
		return
	}
	item := q.req.Questions[q.activeQ]
	if !item.Custom || q.activeOpt != len(item.Options) {
		return
	}
	rs := []rune(q.custom[q.activeQ])
	pos := min(q.customPos[q.activeQ], len(rs))
	if pos == 0 {
		return
	}
	q.custom[q.activeQ] = string(rs[:pos-1]) + string(rs[pos:])
	q.customPos[q.activeQ] = pos - 1
}

func (q *QuestionBar) DeleteForward() {
	if q.req == nil || len(q.req.Questions) == 0 {
		return
	}
	item := q.req.Questions[q.activeQ]
	if !item.Custom || q.activeOpt != len(item.Options) {
		return
	}
	rs := []rune(q.custom[q.activeQ])
	pos := min(q.customPos[q.activeQ], len(rs))
	if pos >= len(rs) {
		return
	}
	q.custom[q.activeQ] = string(rs[:pos]) + string(rs[pos+1:])
	// The deleted rune occupied pos, so the next rune now sits there: the
	// position is unchanged. Assign it like Type and Backspace do, so the
	// three edit operations stay consistent if this ever deletes a range.
	q.customPos[q.activeQ] = pos
}

func (q *QuestionBar) MoveCustomCursor(delta int) {
	if !q.CustomActive() {
		return
	}
	rs := []rune(q.custom[q.activeQ])
	q.customPos[q.activeQ] = min(max(q.customPos[q.activeQ]+delta, 0), len(rs))
}

func (q *QuestionBar) CustomCursorHome() {
	if !q.CustomActive() {
		return
	}
	q.customPos[q.activeQ] = 0
}

func (q *QuestionBar) CustomCursorEnd() {
	if !q.CustomActive() {
		return
	}
	q.customPos[q.activeQ] = len([]rune(q.custom[q.activeQ]))
}

func (q *QuestionBar) CustomActive() bool {
	if q.req == nil || len(q.req.Questions) == 0 {
		return false
	}
	item := q.req.Questions[q.activeQ]
	return item.Custom && q.activeOpt == len(item.Options)
}

func (q *QuestionBar) Submit() {
	if q.req == nil {
		return
	}
	answers := make([][]string, len(q.req.Questions))
	for i, item := range q.req.Questions {
		selected := q.selected[i]
		if len(selected) == 0 && len(item.Options) > 0 {
			selected = map[int]bool{q.activeOpt: true}
		}
		for idx, ok := range selected {
			if !ok {
				continue
			}
			if idx >= 0 && idx < len(item.Options) {
				answers[i] = append(answers[i], item.Options[idx].Label)
			}
		}
		if item.Custom {
			if extra := strings.TrimSpace(q.custom[i]); extra != "" {
				answers[i] = append(answers[i], extra)
			}
		}
	}
	reply := controlruntime.QuestionReply{Answers: answers}
	if q.respond != nil {
		q.respond(reply)
	}
	q.req = nil
	q.respond = nil
}

func (q *QuestionBar) Reject() {
	if q.req == nil {
		return
	}
	reply := controlruntime.QuestionReply{Rejected: true}
	if q.respond != nil {
		q.respond(reply)
	}
	q.req = nil
	q.respond = nil
}

func (q *QuestionBar) Height(width, termHeight int) int {
	if q.req == nil || len(q.req.Questions) == 0 {
		return 0
	}
	contentW := max(40, width-6)
	lines, _ := q.contentLines(contentW, confirmMaxLines(termHeight))
	return len(lines) + 4
}

func (q *QuestionBar) View(width, termHeight int) string {
	if q.req == nil || len(q.req.Questions) == 0 {
		return ""
	}
	barW := max(40, width-4)
	contentW := max(40, width-6)
	maxLines := confirmMaxLines(termHeight)

	title := q.sty.Primary.Bold(true).Render("  Question")
	prefix := "┌─  Question "
	rightLen := max(0, barW-lipgloss.Width(prefix)-1)
	titleBar := q.sty.Border.Render("┌─") + title + " " + q.sty.Border.Render(strings.Repeat(styles.Horizontal, rightLen)+"┐")
	sep := q.sty.Border.Render("├" + strings.Repeat(styles.Horizontal, barW-2) + "┤")
	bottom := q.sty.Border.Render("└" + strings.Repeat(styles.Horizontal, barW-2) + "┘")

	lines, _ := q.contentLines(contentW, maxLines)
	help := "  " + q.sty.Muted.Render("[↑/↓] option  [space] select  [enter] answer  [esc] dismiss")

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", titleBar)
	for _, line := range lines {
		fmt.Fprintf(&b, "%s%s\n", line, strings.Repeat(" ", max(0, barW-lipgloss.Width(line))))
	}
	fmt.Fprintf(&b, "%s\n", sep)
	fmt.Fprintf(&b, "%s%s\n", help, strings.Repeat(" ", max(0, barW-lipgloss.Width(help))))
	b.WriteString(bottom)
	return b.String()
}

func (q *QuestionBar) contentLines(contentW, maxLines int) ([]string, int) {
	item := q.req.Questions[q.activeQ]
	header := strings.TrimSpace(item.Header)
	if header == "" {
		header = fmt.Sprintf("Question %d/%d", q.activeQ+1, len(q.req.Questions))
	}
	lines := []string{}
	for _, line := range wrapText("  "+header, contentW) {
		lines = append(lines, q.sty.Primary.Render(line))
	}
	for _, line := range wrapText("  "+item.Question, contentW) {
		lines = append(lines, q.sty.Base.Render(line))
	}
	for i, opt := range item.Options {
		mark := "( )"
		if q.selected[q.activeQ][i] {
			mark = "(*)"
		}
		cursor := " "
		if i == q.activeOpt {
			cursor = "›"
		}
		label := opt.Label
		if opt.Description != "" {
			label += " - " + opt.Description
		}
		prefix := fmt.Sprintf("  %s %s ", cursor, mark)
		maxW := max(10, contentW-lipgloss.Width(prefix))
		indent := strings.Repeat(" ", lipgloss.Width(prefix))
		for j, line := range wrapText(label, maxW) {
			if j == 0 {
				lines = append(lines, q.sty.Base.Render(prefix+line))
			} else {
				lines = append(lines, q.sty.Base.Render(indent+line))
			}
		}
	}
	customIdx := -1
	if item.Custom {
		prefix := customRowPrefix(q.activeOpt == len(item.Options))
		value := q.custom[q.activeQ]
		customIdx = len(lines)
		if value == "" {
			lines = append(lines, q.sty.Muted.Render(prefix+"type custom answer"))
		} else {
			maxW := max(10, contentW-lipgloss.Width(prefix))
			indent := strings.Repeat(" ", lipgloss.Width(prefix))
			for j, seg := range customSegments([]rune(value), maxW) {
				if j == 0 {
					lines = append(lines, q.sty.Muted.Render(prefix+string(seg)))
				} else {
					lines = append(lines, q.sty.Muted.Render(indent+string(seg)))
				}
			}
		}
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], q.sty.Muted.Render("  ... (truncated)"))
		if customIdx >= maxLines {
			customIdx = -1
		}
	}
	return lines, customIdx
}

// customSegments splits text into display segments that fit within maxW
// columns, breaking at rune boundaries without consuming any rune so cursor
// offsets remain exact.
func customSegments(rs []rune, maxW int) [][]rune {
	if len(rs) == 0 || maxW <= 0 {
		return nil
	}
	var segs [][]rune
	start, w := 0, 0
	for i, r := range rs {
		rw := lipgloss.Width(string(r))
		if w+rw > maxW {
			segs = append(segs, rs[start:i])
			start, w = i, 0
		}
		w += rw
	}
	return append(segs, rs[start:])
}

func customRowPrefix(active bool) string {
	cursor := " "
	if active {
		cursor = "›"
	}
	return fmt.Sprintf("  %s (custom) ", cursor)
}

// Cursor returns the terminal cursor position within the bar's View when the
// custom input row is active, or nil otherwise. The cursor sits at the custom
// input's editing position so typed text is inserted where the user sees it.
func (q *QuestionBar) Cursor(width, termHeight int) *tea.Cursor {
	if !q.CustomActive() {
		return nil
	}
	contentW := max(40, width-6)
	lines, customIdx := q.contentLines(contentW, confirmMaxLines(termHeight))
	if customIdx < 0 {
		return nil
	}
	prefix := customRowPrefix(true)
	prefixW := lipgloss.Width(prefix)
	rs := []rune(q.custom[q.activeQ])
	pos := min(q.customPos[q.activeQ], len(rs))
	segs := customSegments(rs, max(10, contentW-prefixW))
	consumed := 0
	for j, seg := range segs {
		// A position exactly on a wrap boundary belongs to the following row:
		// that is where the rune it precedes is rendered, so the caret sits
		// where the next typed rune will appear. The final segment also takes
		// the end-of-text position.
		if pos < consumed+len(seg) || j == len(segs)-1 {
			y := 1 + customIdx + j // title bar occupies row 0
			if y > len(lines) {
				return nil // cursor row got truncated
			}
			return tea.NewCursor(prefixW+lipgloss.Width(string(rs[consumed:pos])), y)
		}
		consumed += len(seg)
	}
	// Empty input: the caret sits at the start of the placeholder row.
	if customIdx >= len(lines) {
		return nil
	}
	return tea.NewCursor(prefixW, 1+customIdx)
}
