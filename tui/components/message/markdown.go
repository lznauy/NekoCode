package message

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
)

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
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	out, err := getRenderer(width).Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSpace(out)
}
