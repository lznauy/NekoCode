// confirm_bar.go — 确认弹窗栏（yes/no 操作确认）。
package components

import (
	"fmt"
	"strings"

	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"

	"nekocode/common"
)

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

func confirmMaxLines(termHeight int) int {
	n := termHeight / 3
	if n < 6 {
		n = 6
	}
	return n
}

func (c *ConfirmBar) Height(width, termHeight int) int {
	if c.req == nil {
		return 0
	}
	contentW := max(40, width-6)
	maxLines := confirmMaxLines(termHeight)
	n := len(c.descLines(contentW))
	if n > maxLines {
		n = maxLines + 1 // +1 for truncated indicator
	}
	return n + 4 // title (1) + desc lines + level (1) + prompt (1) + border (1)
}

func (c *ConfirmBar) View(width, termHeight int) string {
	if c.req == nil {
		return ""
	}
	barW := max(40, width-4)
	contentW := max(40, width-6)
	maxLines := confirmMaxLines(termHeight)

	title := c.sty.Primary.Bold(true).Render("  Confirm")
	prefix := "┌─  Confirm "
	rightLen := max(0, barW-lipgloss.Width(prefix)-1)
	rightDash := c.sty.Border.Render(strings.Repeat(styles.Horizontal, rightLen) + "┐")
	titleBar := c.sty.Border.Render("┌─") + title + " " + rightDash

	desc := c.formatDesc()
	rawLines := wrapText("  "+desc, contentW)
	var descLines []string
	for i, line := range rawLines {
		if i == 0 {
			descLines = append(descLines, line)
		} else {
			descLines = append(descLines, "  "+line)
		}
	}
	truncated := len(descLines) > maxLines
	if truncated {
		descLines = descLines[:maxLines]
	}

	// Styled action buttons with background colours.
	yesBtn := lipgloss.NewStyle().
		Background(lipgloss.Color(styles.BtnYesBg)).
		Foreground(lipgloss.Color(styles.DiffGreen)).
		Bold(true).
		Padding(0, 2).
		Render("[enter] yes")
	noBtn := lipgloss.NewStyle().
		Background(lipgloss.Color(styles.BtnNoBg)).
		Foreground(lipgloss.Color(styles.BtnNoFg)).
		Padding(0, 2).
		Render("[esc] no")
	levelTag := c.sty.Yellow.Render("["+c.req.Level.String()+"]")
	if c.req.Level == common.LevelForbidden {
		levelTag = c.sty.Red.Render("["+c.req.Level.String()+"]")
	}
	prompt := "  " + levelTag + "  " + c.sty.Base.Render("Proceed?  ") + yesBtn + "  " + noBtn
	promptW := lipgloss.Width(prompt)

	// Separator line between description and actions.
	sep := c.sty.Border.Render("├" + strings.Repeat(styles.Horizontal, barW-2) + "┤")
	bottomBorder := c.sty.Border.Render("└" + strings.Repeat(styles.Horizontal, barW-2) + "┘")

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", titleBar)
	for _, line := range descLines {
		pad := max(0, barW-lipgloss.Width(line))
		fmt.Fprintf(&b, "%s%s\n", c.sty.Base.Render(line), strings.Repeat(" ", pad))
	}
	if truncated {
		fmt.Fprintf(&b, "%s\n", c.sty.Muted.Render("  ... (truncated)"))
	}
	fmt.Fprintf(&b, "%s\n", sep)
	fmt.Fprintf(&b, "%s%s\n", prompt, strings.Repeat(" ", max(0, barW-promptW)))
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
			cmd = truncateCmd(cmd)
			return cmd
		}
	case "write":
		if p, ok := c.req.Args["path"].(string); ok && p != "" {
			return "write " + p
		}
	case "edit":
		if p, ok := c.req.Args["path"].(string); ok && p != "" {
			return "edit " + p
		}
	case "/plugin install":
		if summary, ok := c.req.Args["summary"].(string); ok && summary != "" {
			return summary
		}
		return "Install plugin from " + fmt.Sprint(c.req.Args["source"])
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
		if displayW <= maxW && !strings.ContainsRune(string(remaining), '\n') {
			// Safe to emit as one line: no embedded newlines and fits within maxW.
			lines = append(lines, strings.TrimRight(string(remaining), "\n"))
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

// truncateCmd shortens a bash command for display in the confirm bar.
func truncateCmd(cmd string) string {
	const maxLen = 200
	if len(cmd) <= maxLen {
		return cmd
	}
	// Truncate at a natural boundary to avoid breaking quoted strings mid-token.
	// Prefer the last space (or pipe/semicolon) before maxLen.
	cut := maxLen - 3
	for i := cut; i > maxLen-60 && i > 0; i-- {
		b := cmd[i-1]
		if b == ' ' || b == '|' || b == ';' || b == '&' {
			cut = i
			break
		}
	}
	return cmd[:cut] + "..."
}
