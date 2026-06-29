package app

import (
	"nekocode/bot/skill"
	"nekocode/bot/session"
	"nekocode/common"
)

func (b *Bot) CWD() string                        { return b.sess.CWD() }
func (b *Bot) CurrentSessionID() string           { return b.sess.CurrentID() }
func (b *Bot) SetSession(sess *session.Snapshot)  { b.sess.Set(sess) }
func (b *Bot) ClearContext()                      { b.sess.ClearContext() }
func (b *Bot) SessionMessages() []common.DisplayMessage {
	return b.sess.DisplayMessages()
}

func (b *Bot) ResumeSession(id string) error { return b.sess.Resume(id) }

func (b *Bot) initSession() {
	b.sess = &sessionFacade{}
	b.sess.Init(sessionDeps{
		CWD:        b.cwd,
		CtxMgr:     b.ctxMgr,
		CmdParser:  b.cmdParser,
		SkillState: b.skillState,
		GetAgent:   b.getAgent,
		GetSkills:  func() *skill.Manager { return b.ext.skills },
	})
}
