package bot

import (
	"nekocode/bot/config"
	"nekocode/bot/session"
	"nekocode/bot/skill"
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
	SessionMessages() []common.DisplayMessage
}

type GUI interface {
	UI
	ConfigSnapshot() config.Snapshot
	ApplyConfig(snapshot config.Snapshot) (config.Snapshot, error)
	SkillManagementSnapshot() skill.ManagementSnapshot
	RefreshSkillManagement() skill.ManagementSnapshot
	SetPluginEnabled(name string, enabled bool) (skill.ManagementSnapshot, error)
	CWD() string
	ClearContext()
	CurrentSessionID() string
	SetSession(sess *session.Snapshot)
	ResumeSession(id string) error
}
