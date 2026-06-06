package components

import (
	"fmt"
	"strings"
	"time"

	"nekocode/tui/styles"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	charLimit     = 32768
	maxInputLines = 8
	promptCols    = 2
)

type Input struct {
	textarea      textarea.Model
	width         int
	follow        bool
	sending       bool
	history       []string
	historyIdx    int
	savedInput    string
	historyActive bool
}

func NewInput(width int) *Input {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.SetVirtualCursor(false)
	ta.Focus()
	ta.CharLimit = charLimit
	ta.MaxHeight = maxInputLines
	ta.SetHeight(maxInputLines)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))

	s := ta.Styles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Focused.Placeholder = styles.MutedStyle
	s.Blurred.Placeholder = styles.MutedStyle
	ta.SetStyles(s)

	prompt := styles.CatEyeStyle.Bold(true).Render("┃ ")
	ta.SetPromptFunc(promptCols, func(info textarea.PromptInfo) string { return prompt })
	ta.SetWidth(width)

	return &Input{textarea: ta, width: width, follow: true}
}

func (i *Input) SetWidth(width int) { i.width = width; i.textarea.SetWidth(width) }
func (i *Input) Width() int         { return i.width }

func (i *Input) Value() string { return strings.TrimRight(i.textarea.Value(), "\n\t\r ") }
func (i *Input) SetValue(v string) { i.textarea.SetValue(v) }
func (i *Input) SetCursorEnd()     { i.textarea.MoveToEnd() }

func (i *Input) Reset() {
	i.textarea.Reset()
	i.sending = false
	i.historyActive = false
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

func (i *Input) HistoryUp() {
	if len(i.history) == 0 {
		return
	}
	if i.historyIdx == len(i.history) {
		i.savedInput = i.textarea.Value()
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

func (i *Input) CanCursorUp() bool {
	return i.textarea.Line() > 0 || i.textarea.LineInfo().RowOffset > 0
}

func (i *Input) CanCursorDown() bool {
	info := i.textarea.LineInfo()
	return i.textarea.Line() < i.textarea.LineCount()-1 || info.RowOffset < info.Height-1
}

func (i *Input) visualLines() int {
	text := i.textarea.Value()
	tw := i.width - promptCols
	if tw < 1 {
		tw = 1
	}
	n := 0
	for _, line := range strings.Split(text, "\n") {
		rl := len([]rune(line))
		if rl == 0 {
			n++
		} else {
			n += (rl + tw - 1) / tw
		}
	}
	return n
}

func (i *Input) Height() int { return 4 + min(max(i.visualLines(), 1), maxInputLines) }

func (i *Input) Cursor() *tea.Cursor {
	c := i.textarea.Cursor()
	if c == nil {
		return nil
	}
	return tea.NewCursor(c.Position.X, c.Position.Y+1)
}

func (i *Input) Update(msg tea.Msg) (*Input, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if m.String() == "enter" {
			return i, nil
		}
	}
	var cmd tea.Cmd
	i.textarea, cmd = i.textarea.Update(msg)
	return i, cmd
}

func (i *Input) View() string {
	w := max(20, i.width)
	line := styles.BorderStyle.Render(strings.Repeat(styles.Horizontal, w))

	tv := i.textarea.View()
	if n := min(max(i.visualLines(), 1), maxInputLines); n < maxInputLines {
		lines := strings.Split(tv, "\n")
		if len(lines) > n {
			tv = strings.Join(lines[:n], "\n")
		}
	}

	txt := "Auto"
	if !i.follow {
		txt = "Manual"
	}
	footer := styles.BorderStyle.Render(styles.Vertical+" ") +
		styles.SubtleStyle.Render("Follow:") + " " +
		styles.TealStyle.Render(txt)
	pad := max(0, w-lipgloss.Width(footer)-1)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, "%s\n\n", tv)
	fmt.Fprintf(&b, "%s%s%s\n", footer, strings.Repeat(" ", pad), styles.BorderStyle.Render(styles.Vertical))
	b.WriteString(line)
	return b.String()
}

type TickMsg struct{}

func (i *Input) Init() tea.Cmd { return tea.Batch(textarea.Blink, BlinkTick()) }

func BlinkTick() tea.Cmd {
	return tea.Every(time.Millisecond*500, func(t time.Time) tea.Msg { return TickMsg{} })
}
