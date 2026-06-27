package components

import (
	"fmt"
	"strings"

	"nekocode/common"
	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"
)

type QuestionBar struct {
	req       *common.QuestionRequest
	sty       *styles.Styles
	activeQ   int
	activeOpt int
	selected  map[int]map[int]bool
	custom    []string
}

func NewQuestionBar(sty *styles.Styles) *QuestionBar {
	return &QuestionBar{sty: sty, selected: make(map[int]map[int]bool)}
}

func (q *QuestionBar) SetRequest(req *common.QuestionRequest) {
	q.req = req
	q.activeQ = 0
	q.activeOpt = 0
	q.selected = make(map[int]map[int]bool)
	q.custom = make([]string, len(req.Questions))
}

func (q *QuestionBar) Clear() { q.req = nil }

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
	q.custom[q.activeQ] += text
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
	if len(rs) > 0 {
		q.custom[q.activeQ] = string(rs[:len(rs)-1])
	}
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
	q.req.Response <- common.QuestionReply{Answers: answers}
	q.req = nil
}

func (q *QuestionBar) Reject() {
	if q.req == nil {
		return
	}
	q.req.Response <- common.QuestionReply{Rejected: true}
	q.req = nil
}

func (q *QuestionBar) Height(width, termHeight int) int {
	if q.req == nil || len(q.req.Questions) == 0 {
		return 0
	}
	contentW := max(40, width-6)
	lines := q.contentLines(contentW, confirmMaxLines(termHeight))
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

	lines := q.contentLines(contentW, maxLines)
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

func (q *QuestionBar) contentLines(contentW, maxLines int) []string {
	item := q.req.Questions[q.activeQ]
	header := strings.TrimSpace(item.Header)
	if header == "" {
		header = fmt.Sprintf("Question %d/%d", q.activeQ+1, len(q.req.Questions))
	}
	lines := []string{q.sty.Primary.Render("  " + header)}
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
		lines = append(lines, q.sty.Base.Render(fmt.Sprintf("  %s %s %s", cursor, mark, label)))
	}
	if item.Custom {
		idx := len(item.Options)
		cursor := " "
		if idx == q.activeOpt {
			cursor = "›"
		}
		value := q.custom[q.activeQ]
		if value == "" {
			value = "type custom answer"
		}
		lines = append(lines, q.sty.Muted.Render(fmt.Sprintf("  %s (custom) %s", cursor, value)))
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], q.sty.Muted.Render("  ... (truncated)"))
	}
	return lines
}
