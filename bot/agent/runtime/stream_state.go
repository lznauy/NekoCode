package runtime

import "nekocode/common"

type streamState struct {
	phase      common.PhaseFunc
	text       StreamCallback
	reasoning  ReasoningCallback
	lastReason string
}

func (s *streamState) resetReasoning() {
	s.lastReason = ""
}

func (s *streamState) emitPhase(phase string) {
	if s.phase != nil {
		s.phase(phase)
	}
}

func (s *streamState) emitText(delta string) {
	if s.text != nil {
		s.text(delta, false)
	}
}

func (s *streamState) emitReasoning(delta string) {
	if s.reasoning != nil {
		s.reasoning(delta)
	}
}
