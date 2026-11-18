// suggestions.go — compact command and nested-choice picker below the input.
package components

import (
	"fmt"
	"strings"

	"nekocode/interaction/tui/styles"
	controlruntime "nekocode/runtime"

	"github.com/charmbracelet/x/ansi"
)

const maxVisibleSuggestions = 6

type Suggestions struct {
	title       string
	empty       string
	items       []controlruntime.CommandMenuItem
	selectedIdx int
	scrollOff   int
	visible     bool
	menu        bool
	actionHint  string
	statusHint  string
	sty         *styles.Styles
}

func NewSuggestions(sty *styles.Styles) *Suggestions {
	return &Suggestions{sty: sty}
}

func (s *Suggestions) Refresh(prefix string, commands []controlruntime.CommandMenuItem) {
	s.reset()
	commandPrefix := suggestionPrefix(prefix)
	if commandPrefix == "" {
		return
	}

	p := strings.TrimPrefix(prefix, commandPrefix)
	for _, item := range commands {
		display := commandDisplayName(item.Value)
		if !strings.HasPrefix(display, commandPrefix) {
			continue
		}
		if strings.HasPrefix(strings.TrimPrefix(display, commandPrefix), p) {
			item.Value = display
			if item.Label == "" {
				item.Label = display
			}
			s.items = append(s.items, item)
		}
	}
	if len(s.items) == 1 && s.items[0].Value == prefix {
		return
	}
	if len(s.items) > 0 {
		s.title = "Commands"
		s.visible = true
	}
}

func (s *Suggestions) OpenMenu(title, empty string, items []controlruntime.CommandMenuItem) {
	s.reset()
	s.title = title
	s.empty = empty
	s.items = append([]controlruntime.CommandMenuItem(nil), items...)
	s.menu = true
	s.visible = true
}

func suggestionPrefix(value string) string {
	if strings.HasPrefix(value, "/") {
		return "/"
	}
	if strings.HasPrefix(value, "$") {
		return "$"
	}
	return ""
}

func commandDisplayName(name string) string {
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "$") {
		return name
	}
	return "/" + name
}

func (s *Suggestions) Accept() (controlruntime.CommandMenuItem, bool) {
	if !s.visible || len(s.items) == 0 {
		return controlruntime.CommandMenuItem{}, false
	}
	item := s.items[s.selectedIdx]
	if item.Current {
		// Already active: keep the picker open and tell the user why nothing
		// happened instead of submitting a redundant switch.
		s.statusHint = "already current"
		return item, false
	}
	s.Hide()
	return item, true
}

// Selected returns the currently highlighted menu item without closing the
// picker. It is used by contextual actions such as deleting a saved session.
func (s *Suggestions) Selected() (controlruntime.CommandMenuItem, bool) {
	if !s.visible || !s.menu || len(s.items) == 0 {
		return controlruntime.CommandMenuItem{}, false
	}
	return s.items[s.selectedIdx], true
}

func (s *Suggestions) SetActionHint(hint string) { s.actionHint = hint }

func (s *Suggestions) Cycle(delta int) {
	if !s.visible || len(s.items) == 0 {
		return
	}
	s.statusHint = ""
	s.selectedIdx += delta
	if s.selectedIdx < 0 {
		s.selectedIdx = len(s.items) - 1
	}
	if s.selectedIdx >= len(s.items) {
		s.selectedIdx = 0
	}
	if s.selectedIdx < s.scrollOff {
		s.scrollOff = s.selectedIdx
	}
	if s.selectedIdx >= s.scrollOff+maxVisibleSuggestions {
		s.scrollOff = s.selectedIdx - maxVisibleSuggestions + 1
	}
}

func (s *Suggestions) Visible() bool { return s.visible }
func (s *Suggestions) IsMenu() bool  { return s.visible && s.menu }
func (s *Suggestions) Hide()         { s.reset() }

func (s *Suggestions) reset() {
	s.title = ""
	s.empty = ""
	s.items = nil
	s.selectedIdx = 0
	s.scrollOff = 0
	s.visible = false
	s.menu = false
	s.actionHint = ""
	s.statusHint = ""
}

func (s *Suggestions) Height() int {
	if !s.visible {
		return 0
	}
	n := len(s.items)
	if n == 0 {
		n = 1
	}
	if n > maxVisibleSuggestions {
		n = maxVisibleSuggestions
	}
	return n + 3 // title + rows + breathing room + key hints
}

func (s *Suggestions) View(width int) string {
	return s.ViewWithFooter(width, "")
}

// ViewWithFooter replaces the menu hints with a contextual inline editor.
func (s *Suggestions) ViewWithFooter(width int, footer string) string {
	if !s.visible {
		return ""
	}
	width = max(width, 1)
	title := s.title
	if title == "" {
		title = "Choices"
	}
	header := fmt.Sprintf("── %s", title)
	if len(s.items) > 0 {
		header += fmt.Sprintf(" · %d", len(s.items))
	}
	header += " " + strings.Repeat(styles.Horizontal, max(0, width-ansi.StringWidth(header)-1))

	var b strings.Builder
	b.WriteString(s.sty.Subtle.Render(truncateSuggestion(header, width)))
	if len(s.items) == 0 {
		empty := s.empty
		if empty == "" {
			empty = "No choices available"
		}
		row := s.sty.Border.Render(styles.Vertical) + " " + s.sty.Muted.Render(truncateSuggestion(empty, width-2))
		b.WriteString("\n" + ansi.Truncate(row, width, "…"))
	} else {
		end := min(s.scrollOff+maxVisibleSuggestions, len(s.items))
		labelWidth := s.visibleLabelWidth(end, width)
		for i := s.scrollOff; i < end; i++ {
			b.WriteByte('\n')
			b.WriteString(ansi.Truncate(s.renderRow(s.items[i], i == s.selectedIdx, labelWidth, width), width, "…"))
		}
	}

	if footer != "" {
		fmt.Fprintf(&b, "\n\n%s", footer)
		return b.String()
	}
	b.WriteString("\n\n")
	b.WriteString(s.renderHints(width))
	return b.String()
}

func (s *Suggestions) renderHints(width int) string {
	hints := "↑↓ move  enter select  esc close"
	if s.statusHint != "" {
		hints = s.sty.Yellow.Render(s.statusHint) + "  " + hints
	}
	if item, ok := s.Selected(); ok && item.Key != "" && s.actionHint != "" {
		context := s.sty.Primary.Render(s.actionHint) + "  "
		if ansi.StringWidth(context+hints) > width && s.statusHint == "" {
			hints = "↑↓  enter  esc"
		}
		hints = context + hints
	}
	if s.scrollOff > 0 || s.scrollOff+maxVisibleSuggestions < len(s.items) {
		position := fmt.Sprintf("  %d/%d", s.selectedIdx+1, len(s.items))
		if ansi.StringWidth(hints+position) <= width {
			hints += position
		}
	}
	return ansi.Truncate(s.sty.Subtle.Render(hints), width, "…")
}

func (s *Suggestions) visibleLabelWidth(end, width int) int {
	labelWidth := 0
	for i := s.scrollOff; i < end; i++ {
		labelWidth = max(labelWidth, ansi.StringWidth(s.items[i].Label))
	}
	return min(labelWidth, max(0, min(width-6, max(8, width/3))))
}

func (s *Suggestions) renderRow(item controlruntime.CommandMenuItem, selected bool, labelWidth, width int) string {
	rail, marker := styles.Vertical, "  "
	railStyle, labelStyle := s.sty.Border, s.sty.Muted
	suffix := ""
	if item.Current {
		marker, labelStyle = "✓ ", s.sty.Green
	}
	if selected {
		rail, marker = styles.HeavyVert, "▸ "
		railStyle, labelStyle = s.sty.Primary.Bold(true), s.sty.Primary.Bold(true)
		if item.Current {
			suffix = " ✓"
		}
	}
	label := truncateSuggestion(item.Label, labelWidth)
	label += strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(label)))
	prefix := railStyle.Render(rail) + " " + labelStyle.Render(marker+label)
	used := 1 + 1 + 2 + labelWidth
	if suffix != "" {
		prefix += s.sty.Green.Render(suffix)
		used += ansi.StringWidth(suffix)
	}
	if item.Tag != "" && width-used >= 4 {
		tag := "[" + truncateSuggestion(item.Tag, width-used-3) + "]"
		prefix += " " + s.sty.Muted.Render(tag)
		used += 1 + ansi.StringWidth(tag)
	}
	if item.Description == "" || width-used < 8 {
		return prefix
	}
	description := truncateSuggestion(item.Description, width-used-1)
	return prefix + " " + s.sty.Subtle.Render(description)
}

func truncateSuggestion(value string, width int) string {
	return ansi.Truncate(value, max(0, width), "…")
}
