package message

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

var glamourStyle = []byte(`{"document":{"margin":0}}`)

var (
	mu        sync.Mutex
	renderers = map[int]*glamour.TermRenderer{}
)

func Warmup() {
	mu.Lock()
	defer mu.Unlock()
	renderers = map[int]*glamour.TermRenderer{}
	for w := 40; w <= 160; w++ {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("tokyo-night"),
			glamour.WithStylesFromJSONBytes(glamourStyle),
			glamour.WithWordWrap(w),
		)
		if err != nil {
			panic("failed to warm up markdown renderer: " + err.Error())
		}
		renderers[w] = r
	}
}

func getRenderer(width int) *glamour.TermRenderer {
	mu.Lock()
	defer mu.Unlock()
	if r, ok := renderers[width]; ok {
		return r
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("tokyo-night"),
		glamour.WithStylesFromJSONBytes(glamourStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		panic("failed to create markdown renderer: " + err.Error())
	}
	renderers[width] = r
	return r
}

func RenderMarkdown(content string, width int) string {
	if width <= 0 {
		width = 80
	}
	// Split into blocks at paragraph boundaries, keeping fenced code blocks intact.
	blocks := splitBlocks(content)
	var parts []string
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		out, err := getRenderer(width).Render(block)
		if err != nil {
			parts = append(parts, block)
		} else {
			parts = append(parts, strings.TrimSpace(out))
		}
	}
	return strings.Join(parts, "\n\n")
}

// splitBlocks splits content at "\n\n" paragraph breaks, but preserves
// fenced code blocks (starting with ```) as single units.
func splitBlocks(content string) []string {
	lines := strings.Split(content, "\n")
	var blocks []string
	var cur []string
	inFence := false

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			cur = append(cur, line)
			if !inFence {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = nil
			}
			continue
		}
		if !inFence && line == "" {
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}
