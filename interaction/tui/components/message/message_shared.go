// message_shared.go — 消息渲染共享 helper：缓存、宽度计算、前导空格处理。
package message

import (
	"strings"

	"nekocode/interaction/tui/styles"

	"github.com/charmbracelet/x/ansi"
)

type cachedRender struct {
	rendered string
	width    int
	height   int
}

func fullMessageWidth(available int) int {
	return styles.MessageWidth(available)
}

func stripLeadingSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); {
		if !isTableRenderLine(lines[i]) {
			lines[i] = strings.TrimLeft(lines[i], " ")
			i++
			continue
		}

		start := i
		common := -1
		table := false
		for i < len(lines) && isTableRenderLine(lines[i]) {
			if strings.ContainsRune(ansi.Strip(lines[i]), '┼') {
				table = true
			}
			leading := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
			if common < 0 || leading < common {
				common = leading
			}
			i++
		}
		for j := start; j < i; j++ {
			if !table {
				lines[j] = strings.TrimLeft(lines[j], " ")
				continue
			}
			leading := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
			lines[j] = lines[j][min(leading, common):]
		}
	}
	return strings.Join(lines, "\n")
}

func isTableRenderLine(line string) bool {
	line = ansi.Strip(line)
	return strings.ContainsRune(line, '│') || strings.ContainsRune(line, '┼')
}
