package runtime

import (
	"nekocode/bot/tools"

	"nekocode/common"
)

func (a *Agent) SetConfirmFn(fn common.ConfirmFunc) {
	a.deps.executor.SetConfirmFn(fn)
}

func (a *Agent) ConfirmFn() common.ConfirmFunc {
	return a.deps.executor.ConfirmFn()
}

func (a *Agent) SetPlanMode(on bool) {
	a.deps.executor.SetPlanMode(on)
}

func (a *Agent) ToolExecutionState() *tools.ExecutionState {
	return a.deps.executor.ExecutionState()
}

func (a *Agent) WireTodoWrite(fn common.TodoFunc) {
	if t, err := a.deps.toolRegistry.Get("todo_write"); err == nil {
		if updater, ok := t.(interface{ SetUpdateFn(common.TodoFunc) }); ok {
			updater.SetUpdateFn(fn)
		}
	}
}
