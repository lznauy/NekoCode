// text.go — text utilities: fixed-height rendering, word wrapping, noise filtering.
package processing

import (
	"strings"
	"unicode"

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
		out.WriteString("  " + lineSty.Render(lines[i]))
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
