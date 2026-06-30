package app

import (
	"fmt"
	"os"

	"nekocode/bot/agent/runtime"
	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/skill"
	"nekocode/bot/session"
	"nekocode/common"
)

type sessionFacade struct {
	mgr     *session.Manager
	resumed bool
}

type sessionDeps struct {
	CWD       string
	CtxMgr    *ctxmgr.Manager
	GetAgent  func() *runtime.Agent
	GetSkills func() *skill.Manager
}

func (s *sessionFacade) Init(d sessionDeps) {
	s.mgr = session.NewManager(session.ManagerOptions{
		CWD:     d.CWD,
		Context: d.CtxMgr,
		TokenUsage: func() (int, int) {
			if d.GetAgent == nil {
				return 0, 0
			}
			ag := d.GetAgent()
			if ag == nil {
				return 0, 0
			}
			return ag.TokenUsage()
		},
		AddTokens: func(prompt, completion int) {
			if d.GetAgent == nil {
				return
			}
			ag := d.GetAgent()
			if ag != nil {
				ag.AddTokens(prompt, completion)
			}
		},
		LoadedSkills: func() map[string]bool {
			if d.GetSkills == nil {
				return nil
			}
			sk := d.GetSkills()
			if sk == nil {
				return nil
			}
			return sk.LoadedSet()
		},
		MarkSkillLoaded: func(name string) {
			if d.GetSkills == nil {
				return
			}
			sk := d.GetSkills()
			if sk != nil {
				sk.MarkLoaded(name)
			}
		},
	})

	if err := s.mgr.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "session: %v — running without session persistence\n", err)
	}
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
