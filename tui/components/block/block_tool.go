package block

import (
	"fmt"
	"strings"

	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"
)

func renderToolLine(b ContentBlock, width int, sty *styles.Styles) string {
	running := !b.Done && b.Content == ""

	summary := b.ToolArgs
	if b.Content != "" {
		summary = toolSummary(b)
	}

	toggle := ""
	if running {
		toggle = sty.Yellow.Render("…")
	} else if b.Content != "" {
		if b.Collapsed {
			toggle = sty.Subtle.Render("▸")
		} else {
			toggle = sty.Subtle.Render("▾")
		}
	}

	bullet, bulletStyle := styles.BulletForBlock(b.SubID, b.SubColor, sty.Teal)
	nameStyle := sty.Blue.Bold(true)
	if b.IsError {
		nameStyle = sty.Red.Bold(true)
	} else if running {
		nameStyle = sty.Yellow.Bold(true)
	}
	status := toolStatus(b, sty)
	headPrefix := fmt.Sprintf("%s %s", bulletStyle.Render(bullet), nameStyle.Render(b.ToolName))
	headSuffix := strings.TrimSpace(strings.Join([]string{status, toggle}, " "))
	summaryW := width - lipgloss.Width(headPrefix) - lipgloss.Width(headSuffix) - 6
	if summaryW < 12 {
		summaryW = 12
	}
	summaryText := truncateForWidth(summary, summaryW)
	header := "  " + headPrefix
	if summaryText != "" {
		header += " " + renderSummary(summaryText, sty)
	}
	if headSuffix != "" {
		header += " " + headSuffix
	}
	accentLine := header

	if running || b.Collapsed {
		return accentLine
	}

	contentW := width - 12
	contentW = max(contentW, 10)
	rendered := renderToolContent(b, contentW, sty)
	return lipgloss.JoinVertical(lipgloss.Left, accentLine, renderToolBody(rendered, sty))
}

func toolStatus(b ContentBlock, sty *styles.Styles) string {
	switch {
	case b.IsError:
		return sty.Red.Render("error")
	case !b.Done:
		return sty.Yellow.Render("running")
	default:
		return ""
	}
}

func renderSummary(summary string, sty *styles.Styles) string {
	if strings.HasPrefix(summary, "(+") {
		return sty.Yellow.Render(summary)
	}
	if strings.Contains(summary, "(+") {
		idx := strings.LastIndex(summary, "(+")
		return sty.Muted.Render(summary[:idx]) + " " + sty.Yellow.Render(summary[idx:])
	}
	return sty.Muted.Render(summary)
}

func renderToolBody(rendered string, sty *styles.Styles) string {
	if rendered == "" {
		return ""
	}
	rail := sty.Border.Render(styles.Vertical)
	var out strings.Builder
	for line := range strings.SplitSeq(rendered, "\n") {
		out.WriteString("    ")
		out.WriteString(rail)
		out.WriteString("  ")
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

func truncateForWidth(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	runes := []rune(s)
	var out strings.Builder
	for _, r := range runes {
		next := out.String() + string(r)
		if lipgloss.Width(next)+1 > width {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "…"
}

func editSummary(b ContentBlock) string {
	if idx := strings.LastIndex(b.Content, "(+"); idx >= 0 {
		end := strings.Index(b.Content[idx:], ")")
		if end > 0 {
			return b.Content[idx : idx+end+1]
		}
	}
	return diffChangeSummary(b.Content)
}

func toolSummary(b ContentBlock) string {
	switch b.ToolName {
	case "read":
		return extractReadSummary(b.Content)
	case "edit":
		if s := editSummary(b); s != "" {
			return b.ToolArgs + " " + s
		}
		return b.ToolArgs
	case "diff":
		if s := diffSummary(b); s != "" {
			return s
		}
		return b.ToolArgs
	case "write":
		if s := writeSummary(b); s != "" {
			return s
		}
		return b.ToolArgs
	default:
		return b.ToolArgs
	}
}

// writeSummary extracts info from write output for display in tool header.
func writeSummary(b ContentBlock) string {
	if strings.HasPrefix(b.Content, "[write ") {
		// [write path] header
		if idx := strings.IndexByte(b.Content, ']'); idx > 6 {
			return b.Content[7:idx]
		}
	}
	return diffChangeSummary(b.Content)
}

// diffSummary extracts file path from diff output for display in tool header.
func diffSummary(b ContentBlock) string {
	// New format uses [path#diff] header like edit
	if strings.HasPrefix(b.Content, "[") {
		if idx := strings.IndexByte(b.Content, ']'); idx > 1 {
			header := b.Content[1:idx]
			if hashIdx := strings.LastIndexByte(header, '#'); hashIdx > 0 {
				return header[:hashIdx]
			}
			return header
		}
	}
	return diffChangeSummary(b.Content)
}

func diffChangeSummary(content string) string {
	add, del := countDiffChanges(content)
	if add+del == 0 {
		return ""
	}
	return fmt.Sprintf("(+%d -%d)", add, del)
}

func countDiffChanges(content string) (add, del int) {
	for line := range strings.SplitSeq(content, "\n") {
		if colon := strings.IndexByte(line, ':'); colon > 0 {
			trimmed := strings.TrimLeft(line[:colon], " ")
			if len(trimmed) > 0 && trimmed[0] == '+' {
				add++
			} else if len(trimmed) > 0 && trimmed[0] == '-' {
				del++
			}
		}
	}
	return add, del
}

func extractReadSummary(c string) string {
	if strings.HasPrefix(c, "[") {
		if idx := strings.IndexByte(c, ']'); idx > 1 {
			header := c[1:idx]
			if hashIdx := strings.LastIndexByte(header, '#'); hashIdx > 0 {
				path := header[:hashIdx]
				lines := strings.Split(c[idx+1:], "\n")
				firstLine, lastLine := 0, 0
				for _, l := range lines {
					if colon := strings.IndexByte(l, ':'); colon > 0 {
						var n int
						fmt.Sscanf(l[:colon], "%d", &n)
						if n > 0 {
							if firstLine == 0 {
								firstLine = n
							}
							lastLine = n
						}
					}
				}
				if firstLine > 0 {
					return fmt.Sprintf("%s  L%d-%d", path, firstLine, lastLine)
				}
				return path
			}
		}
	}
	if idx := strings.IndexByte(c, '\n'); idx >= 0 {
		return c[:idx]
	}
	return c
}

func buildPrefix(lineNo int, prefix byte, numFg, redFg, greenFg string) string {
	if lineNo <= 0 {
		return "   "
	}
	var b strings.Builder
	b.WriteString(numFg)
	b.WriteString(pad4(lineNo))
	switch prefix {
	case '-':
		b.WriteString(redFg)
		b.WriteString("- ")
	case '+':
		b.WriteString(greenFg)
		b.WriteString("+ ")
	default:
		b.WriteString("  ")
	}
	return b.String()
}

func renderEditPreview(content string, width int, sty *styles.Styles) string {
	numFg := "\033[38;2;102;102;102m"
	redFg := "\033[38;2;224;108;117m"
	greenFg := "\033[38;2;152;195;121m"
	reset := "\033[0m"

	delLineBg := lipgloss.NewStyle().
		Background(lipgloss.Color(styles.DiffDelBg)).
		Width(width).
		Render
	addLineBg := lipgloss.NewStyle().
		Background(lipgloss.Color(styles.DiffAddBg)).
		Width(width).
		Render

	var out strings.Builder
	for line := range strings.SplitSeq(content, "\n") {
		// Header: [path#TAG] / [write path]
		if isDiffHeaderLine(line) {
			continue
		}
		// Ellipsis: … (N unchanged lines)
		if strings.HasPrefix(line, "…") {
			out.WriteString(sty.Subtle.Render(line))
			out.WriteByte('\n')
			continue
		}
		// "---" separates the diff preview from the full file view
		// (LLM reference). Stop rendering here.
		if strings.TrimSpace(line) == "---" {
			break
		}

		prefix := byte(' ')
		text := line
		lineNo := 0

		if colon := strings.IndexByte(line, ':'); colon > 0 {
			numPart := line[:colon]
			textPart := line[colon+1:]

			// Format: prefix before colon (-NNN: or +NNN: or NNN:)
			trimmed := strings.TrimLeft(numPart, " ")
			if len(trimmed) > 0 && (trimmed[0] == '-' || trimmed[0] == '+') {
				prefix = trimmed[0]
				fmt.Sscanf(trimmed[1:], "%d", &lineNo)
			} else {
				fmt.Sscanf(trimmed, "%d", &lineNo)
			}
			text = textPart
		}

		textFg := func(s string) string { return s }
		if prefix == '-' {
			textFg = func(s string) string { return redFg + s }
		}
		if prefix == '+' {
			textFg = func(s string) string { return greenFg + s }
		}

		prefixStr := buildPrefix(lineNo, prefix, numFg, redFg, greenFg)

		contentLine := prefixStr + textFg(text)

		if prefix == '-' {
			out.WriteString(delLineBg(contentLine))
		} else if prefix == '+' {
			out.WriteString(addLineBg(contentLine))
		} else {
			out.WriteString(contentLine)
			out.WriteString(reset)
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

func isDiffHeaderLine(line string) bool {
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return false
	}
	return strings.Contains(line, "#") || strings.HasPrefix(line, "[write ")
}

func pad4(n int) string {
	s := fmt.Sprintf("%d", n)
	for len(s) < 4 {
		s += " "
	}
	return s + " "
}

func renderToolContent(b ContentBlock, contentW int, sty *styles.Styles) string {
	switch b.ToolName {
	case "read":
		return sty.Muted.MaxWidth(contentW).Render(ParseReadOutput(b.Content))
	case "edit":
		if b.IsError {
			return sty.Muted.MaxWidth(contentW).Render(b.Content)
		}
		return renderEditPreview(b.Content, contentW, sty)
	case "diff":
		// diff uses same format as edit (+NNN:text, -NNN:text, [path#TAG] header)
		if b.IsError {
			return sty.Muted.MaxWidth(contentW).Render(b.Content)
		}
		return renderEditPreview(b.Content, contentW, sty)
	case "write":
		// write uses diff format when showing changes
		if b.IsError {
			return sty.Muted.MaxWidth(contentW).Render(b.Content)
		}
		if hasDiffPreviewContent(b.Content) {
			return renderEditPreview(b.Content, contentW, sty)
		}
		return sty.Muted.MaxWidth(contentW).Render(b.Content)
	case "bash":
		c := strings.TrimSpace(b.Content)
		if c == "" {
			return sty.Subtle.Render("(No output)")
		}
		lines := strings.Split(c, "\n")
		if len(lines) <= 3 {
			return sty.Muted.MaxWidth(contentW).Render(c)
		}
		head := strings.Join(lines[:3], "\n")
		tail := sty.Subtle.Render(fmt.Sprintf("\n... (%d more lines)", len(lines)-3))
		return sty.Muted.MaxWidth(contentW).Render(head) + tail
	default:
		return sty.Muted.MaxWidth(contentW).Render(b.Content)
	}
}

func hasDiffPreviewContent(content string) bool {
	first, _, _ := strings.Cut(content, "\n")
	if strings.HasPrefix(first, "[write ") && strings.HasSuffix(first, "]") {
		return true
	}
	if strings.HasPrefix(first, "[") && strings.HasSuffix(first, "]") && strings.Contains(first, "#") {
		return true
	}
	for line := range strings.SplitSeq(content, "\n") {
		colon := strings.IndexByte(line, ':')
		if colon <= 1 {
			continue
		}
		prefix := line[:colon]
		if (prefix[0] == '+' || prefix[0] == '-') && isDigits(prefix[1:]) {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
