// block_render.go — 块渲染分发。
package block

import (
	"strings"

	"nekocode/tui/styles"
)

// RenderTools 直接渲染工具块列表，每组同名工具可独立折叠。
func RenderTools(blocks []ContentBlock, width int, sty *styles.Styles) string {
	if len(blocks) == 0 {
		return ""
	}
	cardW := width
	if cardW < 20 {
		cardW = 20
	}
	var lines []string
	for _, b := range blocks {
		lines = append(lines, renderToolLine(b, cardW, sty))
	}
	return strings.Join(lines, "\n")
}
