package components

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"nekocode/interaction/tui/styles"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	charLimit               = 32768
	maxInputLines           = 8
	promptCols              = 2
	largePasteCharThreshold = 1000
	largePasteLineThreshold = maxInputLines
)

type pastedBlock struct {
	marker  string
	content string
	start   int
}

type Input struct {
	textarea        textarea.Model
	width           int
	follow          bool
	permissionMode  string
	model           string
	reasoningEffort string
	sending         bool
	history         []string
	historyIdx      int
	savedInput      string
	historyActive   bool
	pastedBlocks    []pastedBlock
	nextPasteID     int
}

func NewInput(width int) *Input {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.SetVirtualCursor(false)
	ta.Focus()
	ta.CharLimit = charLimit
	ta.MaxHeight = maxInputLines
	ta.MinHeight = 1
	ta.DynamicHeight = true
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))

	s := ta.Styles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Focused.Placeholder = styles.MutedStyle
	s.Blurred.Placeholder = styles.MutedStyle
	ta.SetStyles(s)

	prompt := styles.CatEyeStyle.Bold(true).Render("┃ ")
	ta.SetPromptFunc(promptCols, func(info textarea.PromptInfo) string { return prompt })
	ta.SetWidth(max(1, width))

	return &Input{textarea: ta, width: width, follow: true}
}

func (i *Input) SetWidth(width int) { i.width = width; i.textarea.SetWidth(max(1, width)) }
func (i *Input) Width() int         { return i.width }

func (i *Input) Value() string { return strings.TrimRight(i.expandedValue(), "\n\t\r ") }

func (i *Input) SetValue(v string) {
	i.pastedBlocks = nil
	i.nextPasteID = 0
	i.textarea.CharLimit = charLimit
	i.setValueCollapsingLargeContent(v)
	i.syncDisplayCharLimit()
}

func (i *Input) SetCursorEnd()    { i.textarea.MoveToEnd() }
func (i *Input) HasContent() bool { return i.textarea.Value() != "" }

func (i *Input) Clear() {
	i.textarea.Reset()
	i.pastedBlocks = nil
	i.nextPasteID = 0
	i.textarea.CharLimit = charLimit
	i.historyIdx = len(i.history)
	i.savedInput = ""
	i.historyActive = false
}

func (i *Input) Reset() {
	i.Clear()
	i.sending = false
}

func (i *Input) AddHistory(entry string) {
	if entry == "" {
		return
	}
	if len(i.history) > 0 && i.history[len(i.history)-1] == entry {
		return
	}
	i.history = append(i.history, entry)
	i.historyIdx = len(i.history)
}

func (i *Input) SetHistory(entries []string) {
	i.history = append(i.history[:0], entries...)
	i.historyIdx = len(i.history)
	i.historyActive = false
	i.savedInput = ""
}

func (i *Input) History() []string {
	out := make([]string, len(i.history))
	copy(out, i.history)
	return out
}

func (i *Input) HistoryUp() {
	if len(i.history) == 0 {
		return
	}
	if i.historyIdx == len(i.history) {
		i.savedInput = i.Value()
	}
	if i.historyIdx > 0 {
		i.historyIdx--
		i.SetValue(i.history[i.historyIdx])
	}
	i.historyActive = true
}

func (i *Input) HistoryDown() {
	if i.historyIdx >= len(i.history) {
		return
	}
	i.historyIdx++
	if i.historyIdx == len(i.history) {
		i.SetValue(i.savedInput)
		i.historyActive = false
	} else {
		i.SetValue(i.history[i.historyIdx])
	}
}

func (i *Input) SetSending(sending bool) {
	i.sending = sending
	var text string
	if sending {
		text = styles.MutedStyle.Render("⋯ ")
	} else {
		text = styles.CatEyeStyle.Bold(true).Render("┃ ")
	}
	i.textarea.SetPromptFunc(promptCols, func(info textarea.PromptInfo) string { return text })
}

func (i *Input) SetFollow(follow bool) { i.follow = follow }

// SetPermissionMode sets the permission mode shown next to Follow in the
// footer ("manual"/"full"; empty hides the field).
func (i *Input) SetPermissionMode(mode string) { i.permissionMode = mode }

// SetModel sets the current model shown in the footer after Perm.
// Empty hides the field.
func (i *Input) SetModel(model string) { i.model = model }

// SetReasoningEffort sets the configured model effort shown after Model.
// Empty means the provider/model default and is rendered as Auto.
func (i *Input) SetReasoningEffort(effort string) { i.reasoningEffort = strings.TrimSpace(effort) }

func (i *Input) CanCursorUp() bool {
	return i.textarea.Line() > 0 || i.textarea.LineInfo().RowOffset > 0
}

func (i *Input) CanCursorDown() bool {
	info := i.textarea.LineInfo()
	return i.textarea.Line() < i.textarea.LineCount()-1 || info.RowOffset < info.Height-1
}

func (i *Input) Height() int { return 4 + i.textarea.Height() }

func (i *Input) Cursor() *tea.Cursor {
	c := i.textarea.Cursor()
	if c == nil {
		return nil
	}
	return tea.NewCursor(c.X, c.Y+1)
}

func (i *Input) Update(msg tea.Msg) (*Input, tea.Cmd) {
	switch m := msg.(type) {
	case tea.PasteMsg:
		return i.updatePaste(m.Content)
	case tea.KeyPressMsg:
		if m.String() == "enter" {
			return i, nil
		}
		if (key.Matches(m, i.textarea.KeyMap.DeleteCharacterBackward) || key.Matches(m, i.textarea.KeyMap.DeleteWordBackward)) && i.deletePastedBlockAtCursor(true) {
			return i, nil
		}
		if (key.Matches(m, i.textarea.KeyMap.DeleteCharacterForward) || key.Matches(m, i.textarea.KeyMap.DeleteWordForward)) && i.deletePastedBlockAtCursor(false) {
			return i, nil
		}
	}
	cmd, _ := i.applyTextareaUpdate(msg)
	return i, cmd
}

func (i *Input) updatePaste(content string) (*Input, tea.Cmd) {
	remaining := max(0, charLimit-utf8.RuneCountInString(i.expandedValue()))
	content = sanitizeInputLimit(content, remaining)
	if content == "" {
		return i, nil
	}

	if !isLargePaste(content) {
		cmd, _ := i.applyTextareaUpdate(tea.PasteMsg{Content: content})
		return i, cmd
	}

	marker := i.newPasteMarker(content)
	if utf8.RuneCountInString(marker) >= utf8.RuneCountInString(content) {
		cmd, _ := i.applyTextareaUpdate(tea.PasteMsg{Content: content})
		return i, cmd
	}
	start := i.cursorRuneOffset()
	i.nextPasteID++
	cmd, accepted := i.applyTextareaUpdate(tea.PasteMsg{Content: marker})
	if !accepted {
		return i, cmd
	}
	i.pastedBlocks = append(i.pastedBlocks, pastedBlock{marker: marker, content: content, start: start})
	i.syncDisplayCharLimit()
	return i, cmd
}

func (i *Input) setValueCollapsingLargeContent(value string) {
	value = sanitizeInputLimit(value, charLimit)
	if !isLargePaste(value) {
		i.textarea.SetValue(value)
		return
	}
	marker := i.newPasteMarker(value)
	if utf8.RuneCountInString(marker) >= utf8.RuneCountInString(value) {
		i.textarea.SetValue(value)
		return
	}
	i.nextPasteID++
	i.pastedBlocks = append(i.pastedBlocks, pastedBlock{marker: marker, content: value, start: 0})
	i.textarea.SetValue(marker)
}

func (i *Input) newPasteMarker(content string) string {
	lines := strings.Count(content, "\n") + 1
	return fmt.Sprintf("[Pasted Content #%d: %d lines, %d chars]", i.nextPasteID+1, lines, utf8.RuneCountInString(content))
}

func (i *Input) expandedValue() string {
	display := []rune(i.textarea.Value())
	blocks := append([]pastedBlock(nil), i.pastedBlocks...)
	sort.Slice(blocks, func(a, b int) bool { return blocks[a].start < blocks[b].start })
	var expanded strings.Builder
	position := 0
	for _, block := range blocks {
		marker := []rune(block.marker)
		end := block.start + len(marker)
		if block.start < position || block.start < 0 || end > len(display) || string(display[block.start:end]) != block.marker {
			continue
		}
		expanded.WriteString(string(display[position:block.start]))
		expanded.WriteString(block.content)
		position = end
	}
	expanded.WriteString(string(display[position:]))
	return truncateRunes(expanded.String(), charLimit)
}

func (i *Input) applyTextareaUpdate(msg tea.Msg) (tea.Cmd, bool) {
	i.syncDisplayCharLimit()
	before := i.textarea.Value()
	beforeCursor := i.cursorRuneOffset()
	var cmd tea.Cmd
	i.textarea, cmd = i.textarea.Update(msg)
	if !i.reconcileDisplayEdit(before, i.textarea.Value(), beforeCursor) {
		i.textarea.SetValue(before)
		i.setCursorRuneOffset(beforeCursor)
		i.syncDisplayCharLimit()
		return cmd, false
	}
	i.syncDisplayCharLimit()
	return cmd, true
}

// reconcileDisplayEdit keeps paste blocks bound to one exact occurrence. An
// edit may happen before or after a marker, but never inside its visible text.
func (i *Input) reconcileDisplayEdit(before, after string, editCursor int) bool {
	if before == after {
		return true
	}
	oldRunes, newRunes := []rune(before), []rune(after)
	prefix := 0
	suffix := 0
	delta := len(newRunes) - len(oldRunes)
	// Insertions are unambiguous when anchored to the cursor. This matters when
	// the inserted text happens to be identical to a marker next to it.
	if delta > 0 && editCursor >= 0 && editCursor <= len(oldRunes) &&
		editCursor+delta <= len(newRunes) &&
		string(newRunes[:editCursor])+string(newRunes[editCursor+delta:]) == before {
		prefix = editCursor
		suffix = len(oldRunes) - editCursor
	} else {
		for prefix < len(oldRunes) && prefix < len(newRunes) && oldRunes[prefix] == newRunes[prefix] {
			prefix++
		}
		for suffix < len(oldRunes)-prefix && suffix < len(newRunes)-prefix && oldRunes[len(oldRunes)-1-suffix] == newRunes[len(newRunes)-1-suffix] {
			suffix++
		}
	}
	oldEnd := len(oldRunes) - suffix
	for _, block := range i.pastedBlocks {
		end := block.start + utf8.RuneCountInString(block.marker)
		deletesMarker := prefix < end && oldEnd > block.start
		insertsInsideMarker := prefix == oldEnd && prefix > block.start && prefix < end
		if deletesMarker || insertsInsideMarker {
			return false
		}
	}
	newStarts := make([]int, len(i.pastedBlocks))
	for idx := range i.pastedBlocks {
		newStarts[idx] = i.pastedBlocks[idx].start
		if oldEnd <= newStarts[idx] {
			newStarts[idx] += delta
		}
		block := i.pastedBlocks[idx]
		block.start = newStarts[idx]
		marker := []rune(block.marker)
		end := block.start + len(marker)
		if block.start < 0 || end > len(newRunes) || string(newRunes[block.start:end]) != block.marker {
			return false
		}
	}
	for idx := range i.pastedBlocks {
		i.pastedBlocks[idx].start = newStarts[idx]
	}
	return true
}

func (i *Input) deletePastedBlockAtCursor(backward bool) bool {
	display := i.textarea.Value()
	displayRunes := []rune(display)
	cursor := i.cursorRuneOffset()
	for idx, block := range i.pastedBlocks {
		start := block.start
		end := start + utf8.RuneCountInString(block.marker)
		hit := backward && cursor > start && cursor <= end
		if !backward {
			hit = cursor >= start && cursor < end
		}
		if !hit {
			continue
		}

		i.textarea.SetValue(string(append(displayRunes[:start], displayRunes[end:]...)))
		i.setCursorRuneOffset(start)
		i.pastedBlocks = append(i.pastedBlocks[:idx], i.pastedBlocks[idx+1:]...)
		for later := range i.pastedBlocks {
			if i.pastedBlocks[later].start > start {
				i.pastedBlocks[later].start -= end - start
			}
		}
		i.syncDisplayCharLimit()
		return true
	}
	return false
}

func (i *Input) syncDisplayCharLimit() {
	displayLength := utf8.RuneCountInString(i.textarea.Value())
	expandedLength := utf8.RuneCountInString(i.expandedValue())
	hiddenLength := max(0, expandedLength-displayLength)
	i.textarea.CharLimit = max(displayLength, charLimit-hiddenLength)
}

func (i *Input) cursorRuneOffset() int {
	lines := strings.Split(i.textarea.Value(), "\n")
	offset := 0
	for row := 0; row < i.textarea.Line() && row < len(lines); row++ {
		offset += utf8.RuneCountInString(lines[row]) + 1
	}
	return offset + i.textarea.Column()
}

func (i *Input) setCursorRuneOffset(offset int) {
	lines := strings.Split(i.textarea.Value(), "\n")
	i.textarea.MoveToBegin()
	for row, line := range lines {
		lineLen := utf8.RuneCountInString(line)
		if offset <= lineLen || row == len(lines)-1 {
			i.textarea.SetCursorColumn(min(offset, lineLen))
			return
		}
		offset -= lineLen + 1
		i.textarea.CursorDown()
	}
}

func isLargePaste(content string) bool {
	return utf8.RuneCountInString(content) >= largePasteCharThreshold || strings.Count(content, "\n")+1 > largePasteLineThreshold
}

func sanitizeInputLimit(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	var clean strings.Builder
	clean.Grow(min(len(value), limit*4))
	written := 0
	writeRune := func(r rune) bool {
		if written == limit {
			return false
		}
		clean.WriteRune(r)
		written++
		return true
	}
	for offset := 0; offset < len(value) && written < limit; {
		r, size := utf8.DecodeRuneInString(value[offset:])
		offset += size
		switch {
		case r == utf8.RuneError && size == 1:
			continue
		case r == '\r':
			if offset < len(value) && value[offset] == '\n' {
				offset++
			}
			writeRune('\n')
		case r == '\n':
			writeRune('\n')
		case r == '\t':
			for spaces := 0; spaces < 4 && written < limit; spaces++ {
				writeRune(' ')
			}
		case unicode.IsControl(r):
			continue
		default:
			writeRune(r)
		}
	}
	return clean.String()
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (i *Input) View() string {
	w := max(20, i.width)
	line := styles.BorderStyle.Render(strings.Repeat(styles.Horizontal, w))

	tv := strings.TrimRight(i.textarea.View(), "\n")

	txt := "Auto"
	if !i.follow {
		txt = "Manual"
	}
	prefix := styles.BorderStyle.Render(styles.Vertical + " ")
	footer := prefix +
		styles.SubtleStyle.Render("Follow:") + " " +
		styles.TealStyle.Render(txt)
	permission := "Manual"
	permissionStyle := styles.TealStyle
	switch i.permissionMode {
	case "full":
		permission = "FULL"
		permissionStyle = styles.RedStyle.Bold(true)
		footer += styles.SubtleStyle.Render(" · Perm: ") + permissionStyle.Render(permission)
	case "manual", "":
		footer += styles.SubtleStyle.Render(" · Perm: ") + permissionStyle.Render(permission)
	default:
		permission = i.permissionMode
		// Auto mode delegates approval decisions to the judge: make it stand
		// out (muted gray read as "inactive") without the alarm-red of FULL.
		permissionStyle = styles.YellowStyle.Bold(true)
		footer += styles.SubtleStyle.Render(" · Perm: ") + permissionStyle.Render(permission)
	}
	if i.model != "" {
		effort := i.displayReasoningEffort()
		footer += styles.SubtleStyle.Render(" · Effort: ") + styles.YellowStyle.Render(effort)
		footer += styles.SubtleStyle.Render(" · Model: ") + styles.CatEyeStyle.Render(i.model)
	}

	if lipgloss.Width(footer) > w-1 {
		compact := prefix + styles.SubtleStyle.Render("F:") + styles.TealStyle.Render(txt) +
			styles.SubtleStyle.Render(" · P:") + permissionStyle.Render(permission)
		if i.model != "" {
			compact += styles.SubtleStyle.Render(" · E:") + styles.YellowStyle.Render(i.displayReasoningEffort()) +
				styles.SubtleStyle.Render(" · ") + styles.CatEyeStyle.Render(i.model)
		}
		footer = compact
		if lipgloss.Width(footer) > w-1 {
			footer = prefix + styles.SubtleStyle.Render("E:") + styles.YellowStyle.Render(i.displayReasoningEffort()) +
				styles.SubtleStyle.Render(" · F:") + styles.TealStyle.Render(txt) +
				styles.SubtleStyle.Render(" · P:") + permissionStyle.Render(permission) +
				styles.SubtleStyle.Render(" · ") + styles.CatEyeStyle.Render(i.model)
		}
	}
	footer = ansi.Truncate(footer, max(1, w-1), "")
	pad := max(0, w-lipgloss.Width(footer)-1)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, "%s\n\n", tv)
	fmt.Fprintf(&b, "%s%s%s\n", footer, strings.Repeat(" ", pad), styles.BorderStyle.Render(styles.Vertical))
	b.WriteString(line)
	return b.String()
}

func (i *Input) displayReasoningEffort() string {
	if i.reasoningEffort == "" {
		return "Auto"
	}
	return i.reasoningEffort
}

func (i *Input) Init() tea.Cmd { return tea.Batch(textarea.Blink, BlinkTick()) }
