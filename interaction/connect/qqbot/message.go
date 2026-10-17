package qqbot

import (
	"encoding/json"
	"strings"
	"time"
)

// inboundMessage 是解析后的平台消息事件（群 @ / C2C 私聊）。
// 官方平台群聊只投递 @ 消息、私聊全投递，因此入站全部受理。
type inboundMessage struct {
	chatKind  string   // "group" / "c2c"
	chatID    string   // group_openid / user_openid
	msgID     string   // 触发消息 id（被动回复凭证，约 5 分钟有效）
	authorID  string   // member_openid / user_openid
	text      string   // 已 trim，附件记为 [附件]
	imageURLs []string // 图片附件的临时下载 URL（QQ CDN）
}

// sourceID 返回会话标识："group:<group_openid>" / "c2c:<openid>"。
func (m inboundMessage) sourceID() string {
	return m.chatKind + ":" + m.chatID
}

// messageFromEvent 把 gateway Dispatch 事件映射为入站消息；
// 非消息事件（或字段缺失）返回 ok=false。
func messageFromEvent(eventType string, d json.RawMessage) (inboundMessage, bool) {
	var ev struct {
		ID          string `json:"id"`
		Content     string `json:"content"`
		GroupOpenID string `json:"group_openid"`
		Author      struct {
			MemberOpenID string `json:"member_openid"`
			UserOpenID   string `json:"user_openid"`
		} `json:"author"`
		Attachments []struct {
			URL string `json:"url"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(d, &ev); err != nil {
		return inboundMessage{}, false
	}
	var imageURLs []string
	for _, att := range ev.Attachments {
		if att.URL != "" {
			imageURLs = append(imageURLs, att.URL)
		}
	}
	// 图片附件走专用 marker；[附件] 占位只保留给非图片附件。
	text := cleanContent(ev.Content, len(ev.Attachments) > len(imageURLs))
	switch eventType {
	case "GROUP_AT_MESSAGE_CREATE":
		if ev.GroupOpenID == "" {
			return inboundMessage{}, false
		}
		return inboundMessage{
			chatKind:  "group",
			chatID:    ev.GroupOpenID,
			msgID:     ev.ID,
			authorID:  ev.Author.MemberOpenID,
			text:      text,
			imageURLs: imageURLs,
		}, true
	case "C2C_MESSAGE_CREATE":
		if ev.Author.UserOpenID == "" {
			return inboundMessage{}, false
		}
		return inboundMessage{
			chatKind:  "c2c",
			chatID:    ev.Author.UserOpenID,
			msgID:     ev.ID,
			authorID:  ev.Author.UserOpenID,
			text:      text,
			imageURLs: imageURLs,
		}, true
	}
	return inboundMessage{}, false
}

// cleanContent 去掉群 @ 消息前导的 @ 占位空白；有附件时在文本后补 [附件]。
func cleanContent(content string, hasAttachments bool) string {
	text := strings.TrimSpace(content)
	if hasAttachments {
		if text != "" {
			text += " "
		}
		text += "[附件]"
	}
	return text
}

// chatSession 是已知会话及其最近一次触发消息（被动回复凭证）。
type chatSession struct {
	kind  string // "group" / "c2c"
	id    string
	msgID string
	msgAt time.Time
}

// freshMsgID 返回仍在被动回复窗口（约 5 分钟，留 30s 余量）内的触发消息
// id；过期返回 ""，出站降级为主动消息。
func (s chatSession) freshMsgID(now time.Time) string {
	if s.msgID != "" && now.Sub(s.msgAt) < 4*time.Minute+30*time.Second {
		return s.msgID
	}
	return ""
}
