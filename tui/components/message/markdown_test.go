package message

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The problematic text from /tmp/nekocode/nekocode-context.json (message 110).
var problemText = `所有文档链接和图片文件都存在。

总结一下，这次检查发现并修复了 **4 处不一致**：

| 位置 | 问题 | 修复 |
|------|------|------|
| README 第 107 行 | BotInterface 方法数写 11，实际 10 | 11 → 10 |
| README 第 151 行 | 同上 | 11 → 10 |
| docs/ARCHITECTURE.md 第 181 行 | 同上 | 11 → 10 |
| README 第 54 行 | 工具名 ` + "`fetch`/`todo`" + ` 不准确 | → ` + "`web_fetch`/`todo_write`" + ` |

其余内容（安全分级、Hook 数量、事件点数量、子 Agent 类型、Project Info 查询格式、Go 版本、文档链接、图片路径）均与代码一致，没有问题喵~`

func TestRenderMarkdown_CJKNoPrematureWrap(t *testing.T) {
	widths := []int{40, 50, 60, 70, 80, 100, 120}

	Warmup()

	for _, width := range widths {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			out := RenderMarkdown(problemText, width)
			lines := strings.Split(out, "\n")

			for i, line := range lines {
				clean := ansi.Strip(line)
				trimmed := strings.TrimSpace(clean)
				if trimmed == "" {
					continue
				}

				// Table lines skip the check.
				if strings.Contains(clean, "│") || strings.Contains(clean, "┼") ||
					strings.HasPrefix(clean, "─") || strings.Contains(clean, "→") ||
					strings.Contains(clean, "|") {
					continue
				}

				w := ansi.StringWidth(line)
				if w > width {
					t.Errorf("line %d overflows width %d (actual %d): %q",
						i+1, width, w, trimmed)
				}

				// Check for premature wrap: if this is NOT the last non-empty line
				// in the paragraph, it should use at least 60% of the width.
				isLastLine := i == len(lines)-1 ||
					strings.TrimSpace(ansi.Strip(lines[i+1])) == "" ||
					strings.HasPrefix(ansi.Strip(lines[i+1]), "│") ||
					strings.HasPrefix(ansi.Strip(lines[i+1]), "─") ||
					strings.Contains(ansi.Strip(lines[i+1]), "│")

				if !isLastLine && w < width*60/100 && len(trimmed) > 10 {
					t.Errorf("line %d appears prematurely wrapped: width=%d, line_width=%d (%.0f%%, last=%v): %q",
						i+1, width, w, float64(w)/float64(width)*100, isLastLine, trimmed)
				}
			}
		})
	}
}

// Test just the long CJK sentence in isolation (plain text, no markdown).
func TestRenderMarkdown_PlainCJKLine(t *testing.T) {
	longCJK := "其余内容（安全分级、Hook 数量、事件点数量、子 Agent 类型、Project Info 查询格式、Go 版本、文档链接、图片路径）均与代码一致，没有问题喵~"

	Warmup()

	for _, width := range []int{40, 50, 60, 70, 80, 100} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			out := RenderMarkdown(longCJK, width)
			lines := strings.Split(out, "\n")
			t.Logf("=== width=%d ===", width)
			for _, line := range lines {
				clean := ansi.Strip(line)
				w := ansi.StringWidth(line)
				t.Logf("  [%2d] %s", w, clean)

				if strings.TrimSpace(clean) != "" && w > width {
					t.Errorf("overflow: width=%d, line_width=%d: %q", width, w, clean)
				}
			}
		})
	}
}

// Test the full rendering including tables, markdown formatting.
func TestRenderMarkdown_FullOutput(t *testing.T) {
	Warmup()

	widths := []int{60, 80}
	for _, width := range widths {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			out := RenderMarkdown(problemText, width)
			lines := strings.Split(out, "\n")
			t.Logf("=== width=%d (%d lines) ===", width, len(lines))
			for i, line := range lines {
				clean := ansi.Strip(line)
				w := ansi.StringWidth(line)
				t.Logf("  %2d: [%3d] %s", i+1, w, clean)
			}
		})
	}
}

// Benchmarks
func BenchmarkRenderMarkdown_80(b *testing.B) {
	Warmup()
	b.ResetTimer()
	for b.Loop() {
		RenderMarkdown(problemText, 80)
	}
}

func BenchmarkRenderMarkdown_120(b *testing.B) {
	Warmup()
	b.ResetTimer()
	for b.Loop() {
		RenderMarkdown(problemText, 120)
	}
}

func BenchmarkWarmup(b *testing.B) {
	for b.Loop() {
		Warmup()
	}
}
