package app

import (
	"fmt"
	"os"

	"nekocode/bot/app/sessioncmd"
	"nekocode/bot/command"
	"nekocode/bot/session"
)

func (b *Bot) initSession() {
	b.cmdParser = command.NewParser()
	b.skillState = &command.SkillState{MsgStart: -1}

	sess, err := session.New(b.cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session: %v — running without session persistence\n", err)
		return
	}
	b.sess = sess
}

func (b *Bot) initCommands() {
	command.RegisterAll(b.cmdParser, command.Deps{
		CtxMgr:        b.ctxMgr,
		Ag:            b.getAgent,
		SkillReg:      b.skillReg,
		ToolRegistry:  b.toolRegistry,
		ContextWindow: b.cfg.ContextWindow,
		GetConfigFn:   b.ProviderModel,
		ListModelsFn:  b.cfg.AllModelNames,
		FreshStart: func() (string, error) {
			return command.ForceFreshStart(b.ctxMgr, b.skillReg, b.hookReg, b.cfg.ContextWindow)
		},
		SwitchModel: b.SwitchModel,
	}, b.skillState)

	b.registerSessionCommand()
	b.registerExportCommand()
	b.registerPluginCommands()
}

func (b *Bot) registerSessionCommand() {
	b.cmdParser.Register("sessions", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) > 0 {
			id := cmd.Args[0]
			if err := b.ResumeSession(id); err != nil {
				return sessioncmd.ResumeFailed(id, err), true
			}
			b.sessionResumed = true
			return sessioncmd.ResumeSuccess(id, len(b.sess.Messages)), true
		}
		sessions := session.List()
		if len(sessions) == 0 {
			return sessioncmd.FormatSessionList(nil), true
		}
		return sessioncmd.FormatSessionList(sessions), true
	})
}

func (b *Bot) registerExportCommand() {
	b.cmdParser.Register("export", func(cmd *command.Command) (string, bool) {
		msgs := b.ctxMgr.Build(false)
		path, err := sessioncmd.ExportMessages(msgs, sessioncmd.DefaultExportPath)
		if err != nil {
			return sessioncmd.ExportFailed(err), true
		}
		return sessioncmd.ExportSuccess(path, len(msgs)), true
	})
}
