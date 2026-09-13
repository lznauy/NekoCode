// message.go — ChatMessage 类型：用户/助手/系统对话消息的数据载体。
package message

import (
	"nekocode/interaction/tui/components/block"
	"nekocode/protocol"
)

type ChatMessage struct {
	Role            string
	Title           string // optional header label (e.g. "/help", "/compact")
	Content         string
	Reasoning       string
	RenderedContent string
	Blocks          []block.ContentBlock
	Footer          string
	Telemetry       *protocol.Metrics
}
