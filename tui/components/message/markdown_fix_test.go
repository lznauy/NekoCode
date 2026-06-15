package message

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestReproducePrematureWrap(t *testing.T) {
	problemText := `其余内容（安全分级、Hook 数量、事件点数量、子 Agent 类型、Project Info 查询格式、Go 版本、文档链接、图片路径）均与代码一致，没有问题喵~`

	Warmup()

	for _, width := range []int{50, 60, 70, 80, 100} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			out := RenderMarkdown(problemText, width)
			lines := strings.Split(out, "\n")

			for i, line := range lines {
				clean := ansi.Strip(line)
				trimmed := strings.TrimSpace(clean)
				if trimmed == "" {
					continue
				}
				w := ansi.StringWidth(line)
				if w > width {
					t.Errorf("line %d overflows width %d (actual %d): %q",
						i+1, width, w, trimmed)
				}

				isLastLine := i == len(lines)-1 ||
					strings.TrimSpace(ansi.Strip(lines[i+1])) == ""
				if !isLastLine && w < width*50/100 {
					t.Errorf("line %d prematurely wrapped: width=%d, line_width=%d (%.0f%%): %q",
						i+1, width, w, float64(w)/float64(width)*100, trimmed)
				}
			}
		})
	}
}

func TestReproducePrematureWrap_MultipleScenarios(t *testing.T) {
	Warmup()

	cases := []struct {
		name  string
		text  string
		width int
	}{
		{
			"Go_version_wrap",
			"其余内容（安全分级、Hook 数量、事件点数量、子 Agent 类型、Project Info 查询格式、Go 版本、文档链接、图片路径）均与代码一致，没有问题喵~",
			80,
		},
		{
			"Go_version_wrap_narrow",
			"其余内容（安全分级、Hook 数量、事件点数量、子 Agent 类型、Project Info 查询格式、Go 版本、文档链接、图片路径）均与代码一致，没有问题喵~",
			60,
		},
		{
			"short_ascii_before_cjk",
			"Go 版本、文档链接、图片路径",
			30,
		},
		{
			"short_ascii_before_cjk_narrower",
			"Go 版本、文档链接、图片路径",
			20,
		},
		{
			"pure_cjk_no_spaces",
			"版本、文档链接、图片路径均与代码一致没有问题",
			40,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := RenderMarkdown(tc.text, tc.width)
			for _, line := range strings.Split(out, "\n") {
				clean := ansi.Strip(line)
				trimmed := strings.TrimSpace(clean)
				if trimmed == "" {
					continue
				}
				w := ansi.StringWidth(line)
				if w > tc.width {
					t.Errorf("overflow: width=%d, line_width=%d: %q", tc.width, w, trimmed)
				}
			}
		})
	}
}
