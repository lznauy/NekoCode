// processing.go — ProcessingItem：流式渲染 output/thinking 块 + 动态高度。
package processing

import (
	"strings"

	"nekocode/common"
	"nekocode/tui/components/block"
	"nekocode/tui/styles"
)

const (
	thinkLines  = 6 // fixed height for thinking section
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

	blocks       []block.ContentBlock
	thinkingText strings.Builder
	outputText   strings.Builder

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

	subSlots []common.SubSlot // active sub-agents for header rendering
}

func (p *ProcessingItem) SetSkill(s string) { p.skill = s; p.invalidate() }

func NewProcessingItem(sty *styles.Styles) *ProcessingItem {
	return &ProcessingItem{sty: sty, cachedTodosW: -1}
}

func (p *ProcessingItem) SetSpinnerView(view string) { p.spinnerView = view; p.invalidateLight() }
func (p *ProcessingItem) SetStatusText(text string)  { p.statusText = text; p.invalidateLight() }
func (p *ProcessingItem) SetTokens(prompt, completion int) {
	p.tokenPrompt = prompt
	p.tokenCompl = completion
	p.invalidateLight()
}
func (p *ProcessingItem) SetCompactCount(n int) {
	if p.compactCount != n {
		p.compactCount = n
		p.invalidateLight()
	}
}
func (p *ProcessingItem) SetBlocks(blocks []block.ContentBlock) {
	p.blocks = blocks
	p.thinkingText.Reset()
	p.outputText.Reset()
	p.invalidate()
}
func (p *ProcessingItem) SetTodos(text string) {
	if p.todos != text {
		p.todos = text
		p.cachedTodosW = -1
		p.invalidate()
	}
}

func (p *ProcessingItem) AppendThinkingText(delta string) {
	p.thinkingText.WriteString(delta)
	p.invalidateLight()
}
func (p *ProcessingItem) AppendStreamText(delta string) {
	p.outputText.WriteString(delta)
	p.invalidateLight()
}
func (p *ProcessingItem) AddToolBlock(b block.ContentBlock) {
	// Flush any accumulated stream text into a thought block before the
	// tool block. Without this, LLM text that arrives between tool calls
	// accumulates in outputText and leaks into the final assistant answer.
	if out := strings.TrimSpace(p.outputText.String()); out != "" {
		p.blocks = append(p.blocks, block.ContentBlock{
			Type:    block.BlockThought,
			Content: out,
		})
		p.outputText.Reset()
	}
	p.blocks = append(p.blocks, b)
	p.invalidate()
}

func (p *ProcessingItem) AddToolOutput(toolName, output string) {
	p.setLastToolContent(toolName, output)
}

// UpdateToolPreview sets the preview content on the first matching tool block (in creation order).
func (p *ProcessingItem) UpdateToolPreview(toolName, preview string) {
	for i := 0; i < len(p.blocks); i++ {
		b := &p.blocks[i]
		if b.Type == block.BlockTool && b.ToolName == toolName && !b.Done {
			b.Content = preview
			p.invalidate()
			return
		}
	}
}

func (p *ProcessingItem) setLastToolContent(toolName, output string) {
	p.finishToolBlock("", toolName, output)
}

func (p *ProcessingItem) AddThinkBlock(content string) {
	p.blocks = append(p.blocks, block.ContentBlock{Type: block.BlockThought, Content: content})
	p.invalidate()
}
func (p *ProcessingItem) Clear() {
	p.blocks = nil
	p.todos = ""
	p.thinkingText.Reset()
	p.outputText.Reset()
	p.invalidate()
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

func (p *ProcessingItem) OutputText() string   { return p.outputText.String() }
func (p *ProcessingItem) ThinkingText() string { return p.thinkingText.String() }

// AddSubAgent registers a new active sub-agent for header display.
func (p *ProcessingItem) AddSubAgent(id, subType string, colorIdx int) {
	p.subSlots = append(p.subSlots, common.SubSlot{ID: id, SubType: subType, ColorIdx: colorIdx})
	p.invalidateLight()
}

// RemoveSubAgent removes a sub-agent from the header display by ID.
func (p *ProcessingItem) RemoveSubAgent(id string) {
	for i, s := range p.subSlots {
		if s.ID == id {
			p.subSlots = append(p.subSlots[:i], p.subSlots[i+1:]...)
			break
		}
	}
	p.invalidateLight()
}

// AddSubToolOutput sets the output on the last matching sub-agent tool block.
func (p *ProcessingItem) AddSubToolOutput(subID, toolName, output string) {
	p.finishToolBlock(subID, toolName, output)
}

// finishToolBlock finds the first matching tool block (in creation order) and marks it done.
// If subID is non-empty, it also filters by SubID.
func (p *ProcessingItem) finishToolBlock(subID, toolName, output string) {
	for i := 0; i < len(p.blocks); i++ {
		b := &p.blocks[i]
		if b.Type != block.BlockTool || b.ToolName != toolName || b.Done {
			continue
		}
		if subID != "" && b.SubID != subID {
			continue
		}
		if toolName == "edit" {
			// Replace preview with final edit output so relocated/rebased edits
			// render the exact committed diff.
			// formatEditResult returns "[path#TAG]\n..." on success;
			// errors do not start with "[".
			isError := !strings.HasPrefix(output, "[")
			isRevert := strings.Contains(output, "Reverted to pre-edit state")
			b.Content = output
			if isError || isRevert {
				b.IsError = true
			}
		} else {
			b.Content = output
		}
		b.Done = true
		if block.IsPersistent(toolName) {
			b.Collapsed = false
		}
		p.invalidate()
		return
	}
}

// SubSlots returns the current active sub-agent slots.
func (p *ProcessingItem) SubSlots() []common.SubSlot { return p.subSlots }
