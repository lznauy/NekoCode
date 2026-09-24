package block

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"nekocode/interaction/tui/styles"

	"charm.land/lipgloss/v2"
)

const toolOutputEdgeLines = 5

func renderToolLine(b ContentBlock, width int, sty *styles.Styles) string {
	summary := b.ToolArgs
	if b.Content != "" {
		summary = toolSummary(b)
	}

	bullet, bulletStyle := styles.BulletForBlock(b.SubID, b.SubColor, sty.Teal)
	nameStyle := sty.Blue.Bold(true)
	if b.IsError {
		nameStyle = sty.Red.Bold(true)
	}
	headPrefix := fmt.Sprintf("%s %s", bulletStyle.Render(bullet), nameStyle.Render(toolDisplayName(b)))
	accentLine := renderToolHeader(headPrefix, summary, width, sty)

	contentW := width - 12
	contentW = max(contentW, 10)
	rendered := renderToolContent(b, contentW, sty)
	if b.JevNote != "" {
		rendered = sty.Subtle.MaxWidth(contentW).Render(b.JevNote) + "\n" + rendered
	}
	return lipgloss.JoinVertical(lipgloss.Left, accentLine, renderToolBody(rendered, sty))
}

func toolDisplayName(b ContentBlock) string {
	if b.ToolName == "process" {
		switch strings.ToLower(strings.TrimSpace(b.ToolAction)) {
		case "list":
			return "Listed"
		case "wait":
			return "Waited"
		case "watch":
			return "Watched"
		case "stop":
			return "Stopped"
		}
	}
	if b.ToolName == "shell" {
		return "Ran"
	}
	return b.ToolName
}

func renderToolHeader(headPrefix, summary string, width int, sty *styles.Styles) string {
	headerPrefix := "  " + headPrefix
	if summary == "" {
		return headerPrefix
	}

	firstW := width - lipgloss.Width(headerPrefix) - 1
	contPrefix := "    " + sty.Border.Render(styles.Vertical) + " "
	contW := width - lipgloss.Width(contPrefix)
	if contW < 8 {
		contW = 8
	}

	var first string
	var rest []string
	if firstW >= 8 {
		lines := wrapPlainForWidths(summary, firstW, contW)
		if len(lines) > 0 {
			first = lines[0]
			rest = lines[1:]
		}
	} else {
		rest = wrapPlainForWidths(summary, contW, contW)
	}

	var out strings.Builder
	out.WriteString(headerPrefix)
	if first != "" {
		out.WriteByte(' ')
		out.WriteString(renderSummary(first, sty))
	}
	for _, line := range rest {
		out.WriteByte('\n')
		out.WriteString(contPrefix)
		out.WriteString(renderSummary(line, sty))
	}
	return out.String()
}

func renderSummary(summary string, sty *styles.Styles) string {
	if strings.HasPrefix(summary, "(+") {
		return sty.Yellow.Render(summary)
	}
	if strings.Contains(summary, "(+") {
		idx := strings.LastIndex(summary, "(+")
		return sty.Base.Render(summary[:idx]) + " " + sty.Yellow.Render(summary[idx:])
	}
	return sty.Base.Render(summary)
}

func wrapPlainForWidths(s string, firstW, restW int) []string {
	if s == "" {
		return nil
	}
	if firstW <= 0 {
		firstW = restW
	}
	if restW <= 0 {
		restW = firstW
	}
	var lines []string
	remaining := strings.TrimSpace(s)
	width := firstW
	for remaining != "" {
		line, rest := takeLineForWidth(remaining, width)
		lines = append(lines, line)
		remaining = strings.TrimLeft(strings.TrimRight(rest, " \t"), " \t\r\n")
		width = restW
	}
	return lines
}

func takeLineForWidth(s string, width int) (string, string) {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		prefix := strings.TrimRight(s[:idx], " \t\r")
		if width <= 0 || lipgloss.Width(prefix) <= width {
			return prefix, s[idx+1:]
		}
	}
	if width <= 0 || lipgloss.Width(s) <= width {
		return s, ""
	}
	bestByte := -1
	lastByte := 0
	for i, r := range s {
		next := s[:i] + string(r)
		if lipgloss.Width(next) > width {
			break
		}
		lastByte = i + len(string(r))
		if r == ' ' || r == '\t' {
			bestByte = i
		}
	}
	if bestByte > 0 {
		return strings.TrimRight(s[:bestByte], " \t"), s[bestByte+1:]
	}
	if lastByte <= 0 {
		_, size := utf8.DecodeRuneInString(s)
		lastByte = size
	}
	return s[:lastByte], s[lastByte:]
}

func renderToolBody(rendered string, sty *styles.Styles) string {
	if rendered == "" {
		return ""
	}
	return renderToolOutput(rendered, sty)
}

func renderToolOutput(rendered string, sty *styles.Styles) string {
	corner := "└"
	if styles.Vertical == "|" {
		corner = "`"
	}
	var out strings.Builder
	first := true
	for line := range strings.SplitSeq(rendered, "\n") {
		if first {
			out.WriteString("    ")
			out.WriteString(sty.Border.Render(corner))
			out.WriteString("  ")
			first = false
		} else {
			out.WriteString("       ")
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
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
						n, err := strconv.Atoi(strings.TrimSpace(l[:colon]))
						if err == nil && n > 0 {
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
				lineNo, _ = strconv.Atoi(trimmed[1:])
			} else {
				lineNo, _ = strconv.Atoi(trimmed)
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

		switch prefix {
		case '-':
			out.WriteString(delLineBg(contentLine))
		case '+':
			out.WriteString(addLineBg(contentLine))
		default:
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
		if b.IsError {
			return sty.Muted.MaxWidth(contentW).Render(b.Content)
		}
		return renderEditPreview(b.Content, contentW, sty)
	case "write":
		if b.IsError {
			return sty.Muted.MaxWidth(contentW).Render(b.Content)
		}
		if hasDiffPreviewContent(b.Content) {
			return renderEditPreview(b.Content, contentW, sty)
		}
		return sty.Muted.MaxWidth(contentW).Render(b.Content)
	case "shell", "process":
		if strings.TrimSpace(b.Content) == "" {
			return sty.Subtle.Render("(No output)")
		}
		if strings.TrimSpace(b.Content) == "(no managed processes)" {
			return sty.Subtle.Render("No managed processes")
		}
		c := strings.TrimRight(b.Content, "\r\n")
		lines := strings.Split(c, "\n")
		visibleLines := toolOutputEdgeLines * 2
		if len(lines) <= visibleLines {
			return sty.Muted.MaxWidth(contentW).Render(c)
		}
		head := strings.Join(lines[:toolOutputEdgeLines], "\n")
		tail := strings.Join(lines[len(lines)-toolOutputEdgeLines:], "\n")
		marker := sty.Subtle.Render(fmt.Sprintf("... (%d lines truncated) ...", len(lines)-visibleLines))
		return sty.Muted.MaxWidth(contentW).Render(head) + "\n" + marker + "\n" + sty.Muted.MaxWidth(contentW).Render(tail)
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
