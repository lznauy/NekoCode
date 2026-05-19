// types.go — TUI 类型定义：BotInterface、状态枚举、消息类型。
package tui

import (
	"nekocode/common"
)

// BotInterface is the contract any bot implementation must satisfy for the TUI.
type BotInterface interface {
	RunAgent(input string, onStep func(action, toolName, toolArgs, output string)) (string, error)
	ExecuteCommand(input string) (string, bool)
	SkillHint() (string, bool)
	TokenUsage() (prompt, completion int)
	TurnTokenUsage() (prompt, completion int)
	ContextTokens() int
	CompactCount() int
	Duration() string
	CommandNames() []string
	Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc)
	SetCallbacks(textFn, reasonFn func(string))
	Steer(msg string)
	Abort()
	Provider() string
	Model() string
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
