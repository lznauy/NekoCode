package bot

import (
	"nekocode/bot/config"
	"nekocode/bot/session"
	"nekocode/common"
)

type UI interface {
	Run(input string, callbacks common.RunCallbacks) (string, error)
	ExecuteCommand(input string) (string, common.CmdResult)
	SkillHint() (string, bool)
	Stats() common.BotStats
	CommandNames() []string
	Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest, questionFn common.QuestionFunc)
	Steer(msg string)
	Abort()
	ProviderModel() (provider, model string)
	SwitchModel(name string) (model, provider string, err error)
	ContextStatus() string
	ContextReport() string
	ContextSnapshot() common.ContextSnapshot
	SelectSkill(name string) error
	ClearSelectedSkill()
	SessionMessages() []common.DisplayMessage
}

type GUI interface {
	UI
	ConfigView() config.View
	ApplyConfig(view config.View) (config.View, error)
	SkillManagementView() common.SkillManagementView
	RefreshSkillManagement() common.SkillManagementView
	SetPluginEnabled(name string, enabled bool) (common.SkillManagementView, error)
	CWD() string
	ClearContext()
	CurrentSessionID() string
	SetSession(sess *session.Snapshot)
	ResumeSession(id string) error
}
