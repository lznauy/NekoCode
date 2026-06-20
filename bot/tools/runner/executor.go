package runner

import (
	"sync"

	"nekocode/bot/tools/core"
	"nekocode/bot/tools/execution"
	"nekocode/common"
)

// ToolRegistry is the minimal registry contract required by the executor.
type ToolRegistry interface {
	Get(name string) (core.Tool, error)
}

// Previewer is an optional interface for tools that can generate a preview
// before execution.
type Previewer interface {
	Preview(args map[string]any) string
}

type Executor struct {
	registry  ToolRegistry
	state     *execution.ExecutionState
	confirmFn common.ConfirmFunc
	phaseFn   common.PhaseFunc
	planMode  bool
	previewFn func(toolName string, args map[string]any, preview string)
	fnMu      sync.RWMutex
}

func NewExecutor(r ToolRegistry) *Executor {
	return &Executor{registry: r, state: execution.NewExecutionState()}
}

func (e *Executor) ExecutionState() *execution.ExecutionState { return e.state }

func (e *Executor) SetConfirmFn(fn common.ConfirmFunc) {
	e.fnMu.Lock()
	e.confirmFn = fn
	e.fnMu.Unlock()
}

func (e *Executor) ConfirmFn() common.ConfirmFunc {
	e.fnMu.RLock()
	defer e.fnMu.RUnlock()
	return e.confirmFn
}

func (e *Executor) SetPhaseFn(fn common.PhaseFunc) {
	e.fnMu.Lock()
	e.phaseFn = fn
	e.fnMu.Unlock()
}

func (e *Executor) SetPlanMode(on bool) {
	e.fnMu.Lock()
	e.planMode = on
	e.fnMu.Unlock()
}

func (e *Executor) SetPreviewFn(fn func(string, map[string]any, string)) {
	e.fnMu.Lock()
	e.previewFn = fn
	e.fnMu.Unlock()
}
