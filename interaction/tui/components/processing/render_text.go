// render_text.go — text utilities: fixed-height rendering, word wrapping, noise filtering.
package processing

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

func RenderFixed(text string, maxLines int, skipEmpty bool, lineSty lipgloss.Style) string {
	text = strings.TrimRight(text, "\n")
	lines := strings.Split(text, "\n")
	start := 0
	if len(lines) > maxLines {
		start = len(lines) - maxLines
	}
	var out strings.Builder
	for i := start; i < len(lines); i++ {
		if skipEmpty && isEmptyOrNoise(lines[i]) {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString("  ")
		out.WriteString(lineSty.Render(lines[i]))
	}
	if out.Len() == 0 {
		return ""
	}
	out.WriteString("\n")
	return out.String()
}

func WrapPlain(text string, width int) string {
	if width <= 0 {
		return text
	}
	paragraphs := strings.Split(text, "\n")
	var result []string
	for _, para := range paragraphs {
		runes := []rune(para)
		if len(runes) <= width {
			result = append(result, para)
			continue
		}
		for i := 0; i < len(runes); i += width {
			end := i + width
			if end > len(runes) {
				end = len(runes)
			}
			result = append(result, string(runes[i:end]))
		}
	}
	return strings.Join(result, "\n")
}

// isEmptyOrNoise returns true if a line contains only non-content characters
// (dots, dashes, etc.). Empty lines (paragraph breaks) are NOT noise.
func isEmptyOrNoise(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false // paragraph break, keep it
	}
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r > 127 {
			return false
		}
	}
	return true
}

// wrapTail returns only the final wrapped lines. Earlier paragraphs are never
// split or converted to runes. A very long final paragraph is counted once to
// preserve the original wrap alignment, then only its visible suffix is copied.
func wrapTail(text string, width, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	text = strings.TrimRight(text, "\n")
	var parts []string
	remaining := maxLines
	for remaining > 0 {
		start := strings.LastIndexByte(text, '\n') + 1
		paragraph := text[start:]
		lines := 1
		if width > 0 {
			count := utf8.RuneCountInString(paragraph)
			lines = max(1, (count+width-1)/width)
			if lines > remaining {
				keep := count - (lines-remaining)*width
				offset := len(paragraph)
				for j := 0; j < keep; j++ {
					_, size := utf8.DecodeLastRuneInString(paragraph[:offset])
					offset -= size
				}
				paragraph = string([]rune(paragraph[offset:]))
				lines = remaining
			}
			paragraph = WrapPlain(paragraph, width)
		}
		parts = append(parts, paragraph)
		remaining -= lines
		if start == 0 {
			break
		}
		text = text[:start-1]
	}
	for a, b := 0, len(parts)-1; a < b; a, b = a+1, b-1 {
		parts[a], parts[b] = parts[b], parts[a]
	}
	return strings.Join(parts, "\n")
}
