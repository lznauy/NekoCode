package app

import (
	"nekocode/bot/session"
)

// CWD 返回 bot 当前工作目录。
func (b *Bot) CWD() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cwd
}

// CurrentSession 返回当前会话快照。
func (b *Bot) CurrentSession() *session.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sess
}

// CurrentSessionID 返回当前会话 ID；未加载会话时返回空字符串。
func (b *Bot) CurrentSessionID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sess == nil {
		return ""
	}
	return b.sess.ID
}

// SetSession 将指定快照设为当前会话。
func (b *Bot) SetSession(sess *session.Snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sess = sess
}

// ClearContext 清空当前上下文中的消息、待办与压缩边界，保留系统提示和技能。
func (b *Bot) ClearContext() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctxMgr != nil {
		b.ctxMgr.Clear()
	}
}
