// types.go — TUI 类型定义：状态枚举、消息类型。
package tui

import (
	controlruntime "nekocode/runtime"
)

type chatState int

const (
	stateReady chatState = iota
	stateProcessing
	stateConfirming
	stateQuestioning
)

type doneMsg struct {
	evidenceIncomplete bool
	content            string
	metrics            controlruntime.MetricsSnapshot
	err                error
}

type runtimeEventMsg struct {
	event controlruntime.Event
}

type workspaceChangesMsg controlruntime.WorkspaceChanges
