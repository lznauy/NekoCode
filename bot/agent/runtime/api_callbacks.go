package runtime

import "nekocode/common"

func (a *Agent) SetStreamFn(fn StreamCallback) {
	a.stream.text = fn
}

func (a *Agent) SetReasoningStreamFn(fn ReasoningCallback) {
	a.stream.reasoning = fn
}

func (a *Agent) SetPhaseFn(fn common.PhaseFunc) {
	a.stream.phase = fn
	a.deps.executor.SetPhaseFn(fn)
}

func (a *Agent) PhaseFn() common.PhaseFunc {
	return a.stream.phase
}
