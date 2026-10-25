package processing

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/styles"
)

func TestConstrainedCardKeepsMainSections(t *testing.T) {
	sty := styles.DefaultStyles()
	p := NewProcessingItem(&sty)
	p.SetStatusText("Running go test")
	p.SetBlocks([]block.ContentBlock{{Type: block.BlockTool, ToolName: "read", ToolArgs: "main.go", Done: true}, {Type: block.BlockTool, ToolName: "edit", ToolArgs: "main.go", Content: "updated", Done: true}})
	p.SetStreamText("正在验证修改", "核对工具结果")
	p.SetTodos("Tasks 0/1\n▸ 验证接口")
	if p.RenderWithin(80, 100) != p.Render(80) {
		t.Fatal("unconstrained detail differs from the main card")
	}
	for _, height := range []int{30, 15, 11, 8, 4, 2} {
		view := p.RenderWithin(80, height)
		if len(strings.Split(view, "\n")) > height {
			t.Fatalf("card exceeds %d rows", height)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > 80 {
				t.Fatal("card exceeds width")
			}
		}
		plain := ansi.Strip(view)
		if !strings.Contains(plain, "Running go test") {
			t.Fatal("constrained card lost execution status")
		}
		if height >= 11 {
			for _, section := range []string{"activity", "changes", "tasks", "output", "thinking"} {
				if !strings.Contains(plain, section) {
					t.Fatalf("lost %s at height %d: %s", section, height, plain)
				}
			}
		}
	}
	p.SetStreamText("new result", "new evidence")
	if view := ansi.Strip(p.RenderWithin(80, 11)); !strings.Contains(view, "new result") || !strings.Contains(view, "new evidence") {
		t.Fatal("bounded render cache retained old text")
	}
}

func TestDisplayTextFiltersOnlyPresentationNoise(t *testing.T) {
	for _, text := range []string{".", "...", ".\n...\n ", "   "} {
		if DisplayText(text) != "" {
			t.Fatalf("noise remained: %q", text)
		}
	}
	for _, text := range []string{"v1.2", "fmt.Println(\".\")", ".NET", "正常进度", "```go\nfmt.Println(\".\")\n```", "first paragraph\n\nsecond paragraph"} {
		if DisplayText(text) != text {
			t.Fatalf("meaningful text removed: %q", text)
		}
	}
}
