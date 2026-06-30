package runtime

import "sync/atomic"

type tokenMeter struct {
	prompt     atomic.Int64
	completion atomic.Int64
	promptSnap int64
	complSnap  int64
}

func (m *tokenMeter) add(prompt, completion int) {
	m.prompt.Add(int64(prompt))
	m.completion.Add(int64(completion))
}

func (m *tokenMeter) total(contextTokens int) (prompt, completion int) {
	return contextTokens, int(m.completion.Load())
}

func (m *tokenMeter) turn(contextTokens int) (prompt, completion int) {
	return contextTokens - int(m.promptSnap), int(m.completion.Load() - m.complSnap)
}

func (m *tokenMeter) snapshot(contextTokens int) {
	m.promptSnap = int64(contextTokens)
	m.complSnap = m.completion.Load()
}
