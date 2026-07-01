// block.go — 内容块类型与结构体。
package block

import (
	"strings"
)

type BlockType int

const (
	BlockTool BlockType = iota
	BlockThought
)

type ContentBlock struct {
	Type      BlockType
	Content   string
	ToolName  string
	ToolArgs  string
	Collapsed bool
	Done      bool
	IsError   bool   // true when the tool returned an error (used for rendering)
	SubID     string // "" = main agent; non-empty = sub-agent UUID
	SubColor  int    // -1 = main agent; 0-7 = sub-agent color index
}

// FilterFinalBlocks returns persistent tool blocks.
func FilterFinalBlocks(blocks []ContentBlock) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == BlockTool && IsPersistent(b.ToolName) {
			out = append(out, b)
		}
	}
	return out
}

func IsPersistent(toolName string) bool {
	return toolName == "edit" || toolName == "diff" || toolName == "bash" || toolName == "write"
}

// ParseReadOutput extracts the displayable content from read tool output.
// New format: [path#TAG]\nlineNo:content... — skip the header line.
func ParseReadOutput(content string) string {
	// If it starts with [path#TAG] header, skip it for display.
	if strings.HasPrefix(content, "[") {
		if idx := strings.IndexByte(content, '\n'); idx >= 0 {
			// Verify '#' is present to avoid matching code like "[array]\n..."
			if strings.Contains(content[:idx], "#") {
				return content[idx+1:]
			}
		}
	}
	return content
}
