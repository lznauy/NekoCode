// block.go — 内容块类型与结构体。
package block

import (
	"strings"

	"nekocode/protocol"
)

type BlockType int

const (
	BlockTool BlockType = iota
	BlockThought
)

type ContentBlock struct {
	Type     BlockType
	Content  string
	ToolName string
	CallID   string
	JevNote  string // trusted verdict extracted from a tool preview, retained after completion
	ToolArgs string
	// ToolAction preserves action-like tool args that affect display wording.
	// For process this distinguishes list/wait/watch/stop.
	ToolAction string
	Done       bool
	IsError    bool   // true when the tool returned an error (used for rendering)
	SubID      string // "" = main agent; non-empty = sub-agent UUID
	SubColor   int    // -1 = main agent; 0-7 = sub-agent color index
}

// FilterFinalBlocks returns persistent tool blocks.
func FilterFinalBlocks(blocks []ContentBlock) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == BlockTool && (IsPersistent(b.ToolName) || b.JevNote != "") {
			out = append(out, b)
		}
	}
	return out
}

func IsPersistent(toolName string) bool {
	return toolName == "edit" || toolName == "diff" || toolName == "shell" || toolName == "process" || toolName == "write"
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

// SetPreview stores tool-controlled text and trusted decision metadata in
// separate fields so preview content cannot forge a permission verdict.
func (b *ContentBlock) SetPreview(preview string, decision protocol.ToolDecision) {
	b.Content = preview
	if decision == "" {
		return
	}
	note, ok := map[protocol.ToolDecision]string{
		protocol.ToolDecisionJevSafe:           "Jev 判定安全 · 自动放行",
		protocol.ToolDecisionJevDangerous:      "Jev 判定存在风险 · 需要授权确认",
		protocol.ToolDecisionJevURLSafe:        "Jev 判定 URL 安全",
		protocol.ToolDecisionJevURLUnavailable: "Jev 暂不可用 · 按权限规则处理",
		protocol.ToolDecisionJevURLRisky:       "Jev 判定 URL 存在风险 · 需要授权确认",
	}[decision]
	if ok {
		b.JevNote = note
	}
}
