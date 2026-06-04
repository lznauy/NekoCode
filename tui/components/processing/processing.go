// processing.go — ProcessingItem：流式渲染 output/reasoning 块 + 动态高度。
package processing

import (
	"strings"

	"nekocode/tui/components/block"
	"nekocode/tui/styles"
)

const (
	reasonLines = 6 // fixed height for reasoning section
	outputLines = 6 // fixed height for output section
	maxActivity = 5 // max visible activity entries
)

type ProcessingItem struct {

	sty          *styles.Styles
	spinnerView  string
	statusText   string
	skill        string
	tokenPrompt  int
	tokenCompl   int
	compactCount int
	todos        string

	blocks        []block.ContentBlock
	reasoningText strings.Builder
	outputText    strings.Builder

	cachedRender  string
	cachedRenderW int
	cachedHeight  int

	cachedActivity  string
	cachedActivityW int
	cachedActivityN int

	cachedChanges  string
	cachedChangesW int
	cachedChangesN int

	cachedTodos  string
	cachedTodosW int
}

func (p *ProcessingItem) SetSkill(s string) { p.skill = s; p.invalidate() }

func NewProcessingItem(sty *styles.Styles) *ProcessingItem {
	return &ProcessingItem{sty: sty, cachedTodosW: -1}
}

func (p *ProcessingItem) SetSpinnerView(view string)  { p.spinnerView = view; p.invalidateLight() }
func (p *ProcessingItem) SetStatusText(text string)    { p.statusText = text; p.invalidateLight() }
func (p *ProcessingItem) SetTokens(prompt, completion int) {
	p.tokenPrompt = prompt; p.tokenCompl = completion; p.invalidateLight()
}
func (p *ProcessingItem) SetCompactCount(n int) {
	if p.compactCount != n { p.compactCount = n; p.invalidateLight() }
}
func (p *ProcessingItem) SetBlocks(blocks []block.ContentBlock) {
	p.blocks = blocks; p.reasoningText.Reset(); p.outputText.Reset(); p.invalidate()
}
func (p *ProcessingItem) SetTodos(text string) {
	if p.todos != text { p.todos = text; p.cachedTodosW = -1; p.invalidate() }
}

func (p *ProcessingItem) AppendReasoningText(delta string) { p.reasoningText.WriteString(delta); p.invalidateLight() }
func (p *ProcessingItem) AppendStreamText(delta string)    { p.outputText.WriteString(delta); p.invalidateLight() }
func (p *ProcessingItem) AddToolBlock(b block.ContentBlock) {
	if out := p.outputText.String(); out != "" && !strings.HasSuffix(out, "\n") {
		p.outputText.WriteString("\n")
	}
	p.blocks = append(p.blocks, b)
	p.invalidate()
}

func (p *ProcessingItem) AddToolOutput(toolName, output string) {
	p.setLastToolContent(toolName, output)
}

// UpdateToolPreview sets the preview content on the most recent matching tool block.
func (p *ProcessingItem) UpdateToolPreview(toolName, preview string) {
	for i := len(p.blocks) - 1; i >= 0; i-- {
		b := &p.blocks[i]
		if b.Type == block.BlockTool && b.ToolName == toolName && !b.Done {
			b.Content = preview
			p.invalidate()
			return
		}
	}
}

func (p *ProcessingItem) setLastToolContent(toolName, output string) {
	for i := len(p.blocks) - 1; i >= 0; i-- {
		b := &p.blocks[i]
		if b.Type == block.BlockTool && b.ToolName == toolName && !b.Done {
			if toolName != "edit" {
				b.Content = output
			}
			b.Done = true
			if toolName == "edit" || toolName == "bash" || toolName == "write" {
				b.Collapsed = false
			}
			p.invalidate()
			return
		}
	}
}

func (p *ProcessingItem) AddThinkBlock(content string) {
	p.blocks = append(p.blocks, block.ContentBlock{Type: block.BlockThought, Content: content}); p.invalidate()
}
func (p *ProcessingItem) Clear() {
	p.blocks = nil; p.todos = ""; p.reasoningText.Reset(); p.outputText.Reset(); p.invalidate()
}

func (p *ProcessingItem) invalidate() {
	p.cachedRenderW = -1
	p.cachedActivityN = -1
	p.cachedChangesN = -1
}
func (p *ProcessingItem) invalidateLight() { p.cachedRenderW = -1 }

func (p *ProcessingItem) Height(width int) int {
	if p.cachedRenderW != width {
		p.Render(width)
	}
	return p.cachedHeight
}

func (p *ProcessingItem) Blocks() []block.ContentBlock { return p.blocks }

func (p *ProcessingItem) OutputText() string    { return p.outputText.String() }
func (p *ProcessingItem) ReasoningText() string { return p.reasoningText.String() }

func isActivityTool(name string) bool {
	switch name {
	case "edit", "bash", "write":
		return false
	default:
		return true
	}
}
