package app

import (
	"fmt"
	"os"

	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/session"
	"nekocode/common"
)

type sessionFacade struct {
	mgr     *session.Manager
	resumed bool
}

type sessionDeps struct {
	CWD             string
	CtxMgr          *ctxmgr.Manager
	TokenUsage      func() (int, int)
	AddTokens       func(prompt, completion int)
	LoadedSkills    func() map[string]bool
	MarkSkillLoaded func(name string)
}

func newSessionFacade(d sessionDeps) *sessionFacade {
	s := &sessionFacade{}
	s.mgr = session.NewManager(session.ManagerOptions{
		CWD:     d.CWD,
		Context: d.CtxMgr,
		TokenUsage: func() (int, int) {
			if d.TokenUsage == nil {
				return 0, 0
			}
			return d.TokenUsage()
		},
		AddTokens: func(prompt, completion int) {
			if d.AddTokens != nil {
				d.AddTokens(prompt, completion)
			}
		},
		LoadedSkills: func() map[string]bool {
			if d.LoadedSkills == nil {
				return nil
			}
			return d.LoadedSkills()
		},
		MarkSkillLoaded: func(name string) {
			if d.MarkSkillLoaded != nil {
				d.MarkSkillLoaded(name)
			}
		},
	})

	if err := s.mgr.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "session: %v — running without session persistence\n", err)
	}
	return s
}

func (s *sessionFacade) RegisterCommands(p *command.Parser) {
	p.Register("sessions", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) > 0 {
			id := cmd.Args[0]
			sess, err := s.mgr.Resume(id)
			if err != nil {
				return session.ResumeFailed(id, err), true
			}
			s.resumed = true
			return session.ResumeSuccess(id, len(sess.Messages)), true
		}
		return session.FormatSessionList(session.List()), true
	})
	p.Register("export", func(cmd *command.Command) (string, bool) {
		path, msgCount, err := s.mgr.Export(session.DefaultExportPath)
		if err != nil {
			return session.ExportFailed(err), true
		}
		return session.ExportSuccess(path, msgCount), true
	})
}

func (s *sessionFacade) Save() {
	if err := s.mgr.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "session: save error: %v\n", err)
	}
}

func (s *sessionFacade) Resume(id string) error {
	_, err := s.mgr.Resume(id)
	return err
}

func (s *sessionFacade) DrainResumed() bool {
	r := s.resumed
	s.resumed = false
	return r
}

func (s *sessionFacade) CWD() string                { return s.mgr.CWD() }
func (s *sessionFacade) CurrentID() string          { return s.mgr.CurrentID() }
func (s *sessionFacade) Set(sess *session.Snapshot) { s.mgr.Set(sess) }
func (s *sessionFacade) ClearContext()              { s.mgr.ClearContext() }
func (s *sessionFacade) DisplayMessages() []common.DisplayMessage {
	return s.mgr.DisplayMessages()
}

func (b *Bot) CWD() string                       { return b.sess.CWD() }
func (b *Bot) CurrentSessionID() string          { return b.sess.CurrentID() }
func (b *Bot) SetSession(sess *session.Snapshot) { b.sess.Set(sess) }
func (b *Bot) ClearContext()                     { b.sess.ClearContext() }
func (b *Bot) SessionMessages() []common.DisplayMessage {
	return b.sess.DisplayMessages()
}

func (b *Bot) ResumeSession(id string) error { return b.sess.Resume(id) }

func (b *Bot) initSession() {
	b.sess = newSessionFacade(sessionDeps{
		CWD:    b.cwd,
		CtxMgr: b.ctxMgr,
		TokenUsage: func() (int, int) {
			ag := b.getAgent()
			if ag == nil {
				return 0, 0
			}
			return ag.TokenUsage()
		},
		AddTokens: func(prompt, completion int) {
			ag := b.getAgent()
			if ag != nil {
				ag.AddTokens(prompt, completion)
			}
		},
		LoadedSkills: func() map[string]bool {
			if b.ext == nil || b.ext.skills == nil {
				return nil
			}
			return b.ext.skills.LoadedSet()
		},
		MarkSkillLoaded: func(name string) {
			if b.ext != nil && b.ext.skills != nil {
				b.ext.skills.MarkLoaded(name)
			}
		},
	})
}
