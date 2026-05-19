// block.go — 内容块类型与结构体。
package block

import (
	"strings"

	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"
)

type BlockType int

const (
	BlockTool    BlockType = iota
	BlockThought
	BlockReason
)

type ContentBlock struct {
	Type      BlockType
	Content   string
	ToolName  string
	ToolArgs  string
	Collapsed bool
}

var toolAccent = lipgloss.NewStyle().Foreground(lipgloss.Color(styles.Yellow))

func ToolAccent() lipgloss.Style { return toolAccent }

// FilterFinalBlocks 提取所有工具块。
func FilterFinalBlocks(blocks []ContentBlock) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == BlockTool {
			out = append(out, b)
		}
	}
	return out
}

// ParseReadOutput 从 read 工具的结构化输出中提取纯文本内容。
func ParseReadOutput(content string) string {
	start := strings.Index(content, "<![CDATA[")
	if start == -1 {
		return content
	}
	start += len("<![CDATA[")
	end := strings.Index(content[start:], "]]>")
	if end == -1 {
		return content[start:]
	}
	return content[start : start+end]
}

// ParseEditOutput 从 edit 工具输出提取 diff 文本。
func ParseEditOutput(content string) string {
	start := strings.Index(content, "<![CDATA[")
	if start == -1 {
		return content
	}
	start += len("<![CDATA[")
	end := strings.Index(content[start:], "]]>")
	if end == -1 {
		return content[start:]
	}
	return content[start : start+end]
}
