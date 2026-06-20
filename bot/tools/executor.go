package tools

import (
	"nekocode/bot/tools/runner"
)

type Previewer = runner.Previewer
type Executor = runner.Executor

func NewExecutor(r *Registry) *Executor {
	return runner.NewExecutor(r)
}
