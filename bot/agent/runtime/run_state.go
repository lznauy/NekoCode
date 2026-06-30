package runtime

import (
	"nekocode/bot/hooks"

	"nekocode/bot/agent/runtime/control"
)

type runState struct {
	step       int
	stopReason hooks.StopReason
	lastText   string
	finalText  string

	consecutiveHints    int
	consecutiveFailures int
	pendingHints        []hooks.Hint
	gate                *control.ResponseGate
}

func newRunState() runState {
	return runState{
		gate: control.NewResponseGate(),
	}
}

func (s *runState) reset() {
	gate := s.gate
	*s = runState{
		stopReason: hooks.StopCompleted,
	}
	if gate != nil {
		s.gate = gate
		s.gate.Reset()
	} else {
		s.gate = control.NewResponseGate()
	}
}
