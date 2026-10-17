package qqbot

import (
	"testing"
	"time"
)

func TestMessageFromGroupEvent(t *testing.T) {
	d := `{
		"id": "msg-001",
		"content": "  你好 ",
		"group_openid": "G-123",
		"author": {"member_openid": "M-456"},
		"timestamp": "2026-07-28T00:00:00+08:00"
	}`
	msg, ok := messageFromEvent("GROUP_AT_MESSAGE_CREATE", []byte(d))
	if !ok {
		t.Fatal("messageFromEvent() ok = false")
	}
	if msg.chatKind != "group" || msg.chatID != "G-123" {
		t.Fatalf("unexpected chat: %+v", msg)
	}
	if msg.msgID != "msg-001" || msg.authorID != "M-456" {
		t.Fatalf("unexpected ids: %+v", msg)
	}
	if msg.text != "你好" {
		t.Fatalf("text = %q, want 前导空白被 trim", msg.text)
	}
	if msg.sourceID() != "group:G-123" {
		t.Fatalf("sourceID() = %q", msg.sourceID())
	}
}

func TestMessageFromGroupEventWithAttachment(t *testing.T) {
	d := `{
		"id": "msg-002",
		"content": "看图",
		"group_openid": "G-123",
		"author": {"member_openid": "M-456"},
		"attachments": [{"content_type": "image/png", "url": "https://example.com/a.png"}]
	}`
	msg, ok := messageFromEvent("GROUP_AT_MESSAGE_CREATE", []byte(d))
	if !ok {
		t.Fatal("messageFromEvent() ok = false")
	}
	// 图片附件走专用 marker，不再补 [附件] 占位。
	if msg.text != "看图" {
		t.Fatalf("text = %q, want no 附件 placeholder for image", msg.text)
	}
	if len(msg.imageURLs) != 1 || msg.imageURLs[0] != "https://example.com/a.png" {
		t.Fatalf("imageURLs = %v, want the CDN url", msg.imageURLs)
	}

	// 无 URL 的附件（非图片）仍保留 [附件] 占位。
	d = `{
		"id": "msg-004",
		"content": "文件",
		"group_openid": "G-123",
		"author": {"member_openid": "M-456"},
		"attachments": [{"content_type": "file"}]
	}`
	msg, ok = messageFromEvent("GROUP_AT_MESSAGE_CREATE", []byte(d))
	if !ok {
		t.Fatal("messageFromEvent() ok = false")
	}
	if msg.text != "文件 [附件]" {
		t.Fatalf("text = %q, want 附件占位", msg.text)
	}
	if len(msg.imageURLs) != 0 {
		t.Fatalf("imageURLs = %v, want empty", msg.imageURLs)
	}
}

func TestMessageFromC2CEvent(t *testing.T) {
	d := `{
		"id": "msg-003",
		"content": "在吗",
		"author": {"user_openid": "U-789"}
	}`
	msg, ok := messageFromEvent("C2C_MESSAGE_CREATE", []byte(d))
	if !ok {
		t.Fatal("messageFromEvent() ok = false")
	}
	if msg.chatKind != "c2c" || msg.chatID != "U-789" || msg.authorID != "U-789" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if msg.sourceID() != "c2c:U-789" {
		t.Fatalf("sourceID() = %q", msg.sourceID())
	}
}

func TestMessageFromEventRejected(t *testing.T) {
	tests := []struct {
		name string
		typ  string
		d    string
	}{
		{"频道消息 v1 不处理", "AT_MESSAGE_CREATE", `{"id":"m","channel_id":"c","author":{"id":"u"},"content":"hi"}`},
		{"未知事件", "MESSAGE_CREATE", `{"id":"m"}`},
		{"群消息缺 group_openid", "GROUP_AT_MESSAGE_CREATE", `{"id":"m","content":"hi"}`},
		{"私聊缺 user_openid", "C2C_MESSAGE_CREATE", `{"id":"m","content":"hi","author":{}}`},
		{"非法 JSON", "GROUP_AT_MESSAGE_CREATE", `not-json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := messageFromEvent(tt.typ, []byte(tt.d)); ok {
				t.Fatal("messageFromEvent() ok = true, want false")
			}
		})
	}
}

func TestMessageBody(t *testing.T) {
	withID := messageBody("你好", "msg-001")
	if withID["msg_type"] != 0 || withID["content"] != "你好" || withID["msg_id"] != "msg-001" {
		t.Fatalf("unexpected body: %+v", withID)
	}

	withoutID := messageBody("你好", "")
	if withoutID["msg_type"] != 0 || withoutID["content"] != "你好" {
		t.Fatalf("unexpected body: %+v", withoutID)
	}
	if _, has := withoutID["msg_id"]; has {
		t.Fatal("msg_id 为空时不应出现在 body 中（主动消息）")
	}
}

func TestFreshMsgID(t *testing.T) {
	now := time.Now()
	fresh := chatSession{msgID: "msg-001", msgAt: now.Add(-2 * time.Minute)}
	if got := fresh.freshMsgID(now); got != "msg-001" {
		t.Fatalf("freshMsgID() = %q, want msg-001", got)
	}
	// 超过被动回复窗口（约 5 分钟，留 30s 余量）降级为主动消息。
	stale := chatSession{msgID: "msg-001", msgAt: now.Add(-5 * time.Minute)}
	if got := stale.freshMsgID(now); got != "" {
		t.Fatalf("freshMsgID() = %q, want empty", got)
	}
	empty := chatSession{msgAt: now}
	if got := empty.freshMsgID(now); got != "" {
		t.Fatalf("freshMsgID() = %q, want empty", got)
	}
}
