// types.go — TUI 类型定义：状态枚举、消息类型。
package tui

import (
	"nekocode/common"
)

type notifyMsg struct {
	content string
}

type chatState int

const (
	stateReady chatState = iota
	stateProcessing
	stateConfirming
	stateQuestioning
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

type questionMsg struct {
	req common.QuestionRequest
}
