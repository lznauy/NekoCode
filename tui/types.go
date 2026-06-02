// types.go — TUI 类型定义：BotInterface、状态枚举、消息类型。
package tui

import (
	"nekocode/common"
)

// BotInterface is the contract any bot implementation must satisfy for the TUI.
type BotInterface interface {
	RunAgent(input string, onStep func(action, toolName, toolArgs, output string)) (string, error)
	ExecuteCommand(input string) (string, common.CmdResult)
	SkillHint() (string, bool)
	Stats() common.BotStats
	CommandNames() []string
	Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest)
	SetCallbacks(textFn, reasonFn func(string))
	Steer(msg string)
	Abort()
	ProviderModel() (provider, model string)
	SwitchModel(name string) (model, provider string, err error)
}

type notifyMsg struct {
	content string
}

type chatState int

const (
	stateReady chatState = iota
	stateProcessing
	stateConfirming
)

type doneMsg struct {
	content  string
	duration string
	tokens   string
	err      error
}

type confirmMsg struct {
	req common.ConfirmRequest
}
