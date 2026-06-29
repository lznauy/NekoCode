package app

import (
	"fmt"
	"os"

	"nekocode/bot/command"
	"nekocode/bot/session"
	"nekocode/common"
)

// CWD 返回 bot 当前工作目录。
func (b *Bot) CWD() string {
	return b.sessions.CWD()
}

// CurrentSession 返回当前会话快照。
func (b *Bot) CurrentSession() *session.Snapshot {
	return b.sessions.Current()
}

// CurrentSessionID 返回当前会话 ID；未加载会话时返回空字符串。
func (b *Bot) CurrentSessionID() string {
	return b.sessions.CurrentID()
}

// SetSession 将指定快照设为当前会话。
func (b *Bot) SetSession(sess *session.Snapshot) {
	b.sessions.Set(sess)
}

// ClearContext 清空当前上下文中的消息、待办与压缩边界，保留系统提示和技能。
func (b *Bot) ClearContext() {
	b.sessions.ClearContext()
}

func (b *Bot) SessionMessages() []common.DisplayMessage {
	return b.sessions.DisplayMessages()
}

func (b *Bot) initSession() {
	b.cmdParser = command.NewParser()
	b.skillState = &command.SkillState{MsgStart: -1}
	b.sessions = session.NewManager(session.ManagerOptions{
		CWD:     b.cwd,
		Context: b.ctxMgr,
		TokenUsage: func() (int, int) {
			if b.ag == nil {
				return 0, 0
			}
			return b.getAgent().TokenUsage()
		},
		AddTokens: func(prompt, completion int) {
			if b.ag != nil {
				b.getAgent().AddTokens(prompt, completion)
			}
		},
		LoadedSkills: func() map[string]bool {
			if b.skills == nil {
				return nil
			}
			return b.skills.LoadedSet()
		},
		MarkSkillLoaded: func(name string) {
			if b.skills != nil {
				b.skills.MarkLoaded(name)
			}
		},
	})

	if err := b.sessions.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "session: %v — running without session persistence\n", err)
	}
}

func (b *Bot) registerSessionCommand() {
	b.cmdParser.Register("sessions", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) > 0 {
			id := cmd.Args[0]
			sess, err := b.sessions.Resume(id)
			if err != nil {
				return session.ResumeFailed(id, err), true
			}
			b.sessionResumed = true
			return session.ResumeSuccess(id, len(sess.Messages)), true
		}
		return session.FormatSessionList(session.List()), true
	})
}

func (b *Bot) registerExportCommand() {
	b.cmdParser.Register("export", func(cmd *command.Command) (string, bool) {
		path, msgCount, err := b.sessions.Export(session.DefaultExportPath)
		if err != nil {
			return session.ExportFailed(err), true
		}
		return session.ExportSuccess(path, msgCount), true
	})
}

func (b *Bot) saveSession() {
	if err := b.sessions.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "session: save error: %v\n", err)
	}
}

func (b *Bot) ResumeSession(id string) error {
	_, err := b.sessions.Resume(id)
	return err
}
