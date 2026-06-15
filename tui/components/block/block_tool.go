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
		if b.Collapsed {
			summary = toolSummary(b)
		} else if b.ToolName == "edit" {
			summary = toolSummary(b)
		}
	}

	arrow := ""
	if running {
		arrow = " " + sty.Subtle.Render("\u2026")
	} else if b.Content != "" {
		if b.Collapsed {
			arrow = " " + sty.Subtle.Render("[+]")
		} else {
			arrow = " " + sty.Subtle.Render("[-]")
		}
	}

	bullet, bulletStyle := styles.BulletForBlock(b.SubID, b.SubColor, sty.Teal)
	header := fmt.Sprintf("%s %s %s%s", bullet, b.ToolName, summary, arrow)
	accentLine := "  " + bulletStyle.Render(header)

	if running || b.Collapsed {
		return accentLine
	}

	contentW := width - 6
	contentW = max(contentW, 10)
	rendered := renderToolContent(b, contentW, sty)
	indented := lipgloss.NewStyle().PaddingLeft(2).Render(rendered)
	return lipgloss.JoinVertical(lipgloss.Left, accentLine, indented)
}

func editSummary(b ContentBlock) string {
	if idx := strings.LastIndex(b.Content, "(+"); idx >= 0 {
		end := strings.Index(b.Content[idx:], ")")
		if end > 0 {
			return b.Content[idx : idx+end+1]
		}
	}
	add, del := 0, 0
	for line := range strings.SplitSeq(b.Content, "\n") {
		if colon := strings.IndexByte(line, ':'); colon > 0 {
			trimmed := strings.TrimLeft(line[:colon], " ")
			if len(trimmed) > 0 && trimmed[0] == '+' {
				add++
			} else if len(trimmed) > 0 && trimmed[0] == '-' {
				del++
			}
		}
	}
	if add+del > 0 {
		return fmt.Sprintf("(+%d -%d)", add, del)
	}
	return ""
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
	default:
		return b.ToolArgs
	}
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
		// Header: [path#TAG] — must contain '#' to avoid matching code like "[array]"
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && strings.Contains(line, "#") {
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
		// formatEditResult (diff.go) returns "[path#TAG]\n+NNN: ..." on
		// success and goes through renderEditPreview.  Errors (e.g.
		// "file X has not been read yet") are plain text — render them
		// with sty.Muted like every other tool.  finishToolBlock sets
		// IsError when the output does not start with "[".
		if b.IsError {
			return sty.Muted.MaxWidth(contentW).Render(b.Content)
		}
		return renderEditPreview(b.Content, contentW, sty)
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
