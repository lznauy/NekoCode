// confirm_bar.go — 确认弹窗栏（yes/no 操作确认）。
package components

import (
	"strings"

	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"

	"nekocode/common")

type ConfirmBar struct {
	req *common.ConfirmRequest
	sty *styles.Styles
}

func NewConfirmBar(sty *styles.Styles) *ConfirmBar {
	return &ConfirmBar{sty: sty}
}

func (c *ConfirmBar) SetRequest(req *common.ConfirmRequest) { c.req = req }
func (c *ConfirmBar) Clear()                                { c.req = nil }
func (c *ConfirmBar) Respond(ok bool)                       { c.req.Response <- ok; c.req = nil }

func (c *ConfirmBar) Height(width int) int {
	if c.req == nil {
		return 0
	}
	contentW := max(40, width-6)
	lines := c.descLines(contentW)
	// title (1) + desc lines + level (1) + prompt (1) + border (1).
	return len(lines) + 4
}

func (c *ConfirmBar) View(width int) string {
	if c.req == nil {
		return ""
	}
	// Leave room for padding and borders.
	contentW := max(40, width-6)

	title := c.sty.Primary.Bold(true).Render("Confirm")
	topBorder := c.sty.Border.Render(styles.Horizontal)
	barW := max(40, width-4)
	titleBar := topBorder + " " + title + " " + strings.Repeat(styles.Horizontal, max(0, barW-lipgloss.Width(title)-2))

	// Show the full command, line-wrapped.
	descLines := c.descLines(contentW)

	level := c.sty.Yellow.Render(c.req.Level.String())
	if c.req.Level == common.LevelForbidden {
		level = c.sty.Red.Render(c.req.Level.String())
	}

	hint := c.sty.Primary.Bold(true).Render("[enter] yes") + "  " + c.sty.Muted.Render("[esc] no")
	prompt := c.sty.Base.Render("  Proceed?  ") + hint
	promptW := lipgloss.Width(prompt)

	bottomBorder := c.sty.Border.Render(strings.Repeat(styles.Horizontal, barW))

	var b strings.Builder
	b.WriteString(titleBar + "\n")
	for _, line := range descLines {
		lineW := lipgloss.Width(line)
		b.WriteString(c.sty.Base.Render(line) + strings.Repeat(" ", max(0, barW-lineW)) + "\n")
	}
	b.WriteString("  [" + level + "]\n")
	b.WriteString(prompt + strings.Repeat(" ", max(0, barW-promptW)) + "\n")
	b.WriteString(bottomBorder)

	return b.String()
}

// descLines formats the tool description into wrapped lines.
func (c *ConfirmBar) descLines(maxW int) []string {
	desc := c.formatDesc()
	if desc == "" {
		return nil
	}
	return wrapText("  "+desc, maxW)
}

// formatDesc builds a human-readable description of the tool being confirmed.
func (c *ConfirmBar) formatDesc() string {
	switch c.req.ToolName {
	case "bash":
		if cmd, ok := c.req.Args["command"].(string); ok && cmd != "" {
			return c.req.ToolName + " " + cmd
		}
	case "write", "edit":
		if p, ok := c.req.Args["path"].(string); ok && p != "" {
			return c.req.ToolName + " " + p
		}
	}
	if p, ok := c.req.Args["path"].(string); ok && p != "" {
		return c.req.ToolName + " " + p
	}
	return c.req.ToolName
}

// wrapText wraps text to fit within maxW display width.
// Breaks at spaces when possible, hard-breaks otherwise.
func wrapText(text string, maxW int) []string {
	if maxW <= 0 {
		return []string{text}
	}
	var lines []string
	remaining := []rune(text)
	for len(remaining) > 0 {
		displayW := lipgloss.Width(string(remaining))
		if displayW <= maxW {
			lines = append(lines, string(remaining))
			break
		}
		// Find how many runes fit within maxW.
		cut := 0
		w := 0
		lastSpace := -1
		for i, r := range remaining {
			if r == '\n' {
				lines = append(lines, string(remaining[:i]))
				remaining = remaining[i+1:]
				cut = -1
				break
			}
			rw := lipgloss.Width(string(r))
			if w+rw > maxW {
				break
			}
			w += rw
			cut = i + 1
			if r == ' ' {
				lastSpace = i
			}
		}
		if cut < 0 {
			continue
		}
		// Prefer breaking at the last space.
		if lastSpace > 0 && lastSpace < cut {
			cut = lastSpace
		}
		if cut == 0 {
			cut = 1 // at least one rune
		}
		lines = append(lines, strings.TrimRight(string(remaining[:cut]), " "))
		remaining = remaining[cut:]
		// Trim leading spaces on continuation lines.
		if len(remaining) > 0 && remaining[0] == ' ' {
			remaining = remaining[1:]
		}
	}
	return lines
}
